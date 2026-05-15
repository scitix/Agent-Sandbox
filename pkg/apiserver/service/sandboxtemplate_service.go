// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// errVersionNotIncreasing is a sentinel used to distinguish a business-rule
// rejection (version must be strictly greater) from transient I/O errors
// inside a retry.RetryOnConflict loop.
var errVersionNotIncreasing = errors.New("version not increasing")

func isVersionNotIncreasing(err error) bool {
	return errors.Is(err, errVersionNotIncreasing)
}

// SandboxTemplateService defines business operations for SandboxTemplates.
type SandboxTemplateService interface {
	// List returns templates visible to the caller (auth). isAdmin=true bypasses
	// visibility filtering and returns all templates.
	List(ctx context.Context, auth domain.AuthInfo, isAdmin bool) ([]domain.SandboxTemplate, *domain.AppError)
	// Get returns a single template if it is visible to the caller. isAdmin=true
	// bypasses visibility filtering. Returns ErrCodeNotFound when the template
	// does not exist or the caller cannot see it (to avoid leaking names).
	Get(ctx context.Context, name string, auth domain.AuthInfo, isAdmin bool) (*domain.SandboxTemplate, *domain.AppError)
	// Admin only:
	Create(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) (*domain.SandboxTemplate, *domain.AppError)
	Update(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) (*domain.SandboxTemplate, *domain.AppError)
	Delete(ctx context.Context, name string) *domain.AppError
	// CreateOrUpdate upserts a SandboxTemplate. It is used by SyncService to apply
	// sync events from ws-proxy. The operation is idempotent: if the template already
	// exists its spec is replaced; otherwise it is created.
	CreateOrUpdate(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) *domain.AppError
	// StripStaleGlobalLabels removes the sync-source=global label from any local
	// template whose name is NOT present in knownNames. This corrects templates
	// that were mistakenly (or historically) labeled as global but no longer exist
	// on the master cluster, preventing delete requests from being forwarded to
	// ws-proxy and receiving a 404.
	StripStaleGlobalLabels(ctx context.Context, knownNames map[string]struct{}) *domain.AppError
}

type k8sSandboxTemplateService struct {
	client client.Client
}

// NewSandboxTemplateService creates a new SandboxTemplateService backed by the given K8s client.
func NewSandboxTemplateService(c client.Client) SandboxTemplateService {
	return &k8sSandboxTemplateService{client: c}
}

func (s *k8sSandboxTemplateService) List(ctx context.Context, auth domain.AuthInfo, isAdmin bool) ([]domain.SandboxTemplate, *domain.AppError) {
	list := &agentsv1alpha1.SandboxTemplateList{}
	if err := s.client.List(ctx, list); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	items := make([]domain.SandboxTemplate, 0, len(list.Items))
	for i := range list.Items {
		if isAdmin || isVisible(list.Items[i].Spec.Visibility, auth) {
			items = append(items, templateFromCRD(ctx, &list.Items[i]))
		}
	}
	// Sort by name for consistent ordering
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *k8sSandboxTemplateService) Get(ctx context.Context, name string, auth domain.AuthInfo, isAdmin bool) (*domain.SandboxTemplate, *domain.AppError) {
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: name}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if !isAdmin && !isVisible(tmpl.Spec.Visibility, auth) {
		// Return 404 to avoid leaking the existence of restricted templates.
		return nil, domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", name))
	}
	result := templateFromCRD(ctx, tmpl)
	return &result, nil
}

func (s *k8sSandboxTemplateService) Create(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) (*domain.SandboxTemplate, *domain.AppError) {
	obj := tmpl.DeepCopy()
	obj.ResourceVersion = ""
	if err := s.client.Create(ctx, obj); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil, domain.NewConflict(fmt.Sprintf("sandbox template %q already exists", tmpl.Name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	result := templateFromCRD(ctx, obj)
	return &result, nil
}

func (s *k8sSandboxTemplateService) Update(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) (*domain.SandboxTemplate, *domain.AppError) {
	// Semver validation is done once upfront (before any retries) since it only
	// depends on the caller-supplied input, not on the current object state.
	newVer := tmpl.Spec.Version
	if newVer != "" {
		if err := validateSemver(newVer); err != nil {
			return nil, domain.NewBadRequest("version must be in x.y.z format: " + err.Error())
		}
	}

	// Optimistic-lock path: caller supplied a resourceVersion — perform a single
	// Update (no retry). On conflict the caller must refresh and resubmit.
	if tmpl.ResourceVersion != "" {
		base := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: tmpl.Name}, base); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", tmpl.Name))
			}
			return nil, domain.NewInternal(err.Error(), err)
		}
		if newVer != "" && base.Spec.Version != "" {
			cmp, cmpErr := compareSemver(newVer, base.Spec.Version)
			if cmpErr == nil && cmp <= 0 {
				return nil, domain.NewBadRequest(fmt.Sprintf("version %q must be greater than current version %q", newVer, base.Spec.Version))
			}
		}
		updated := base.DeepCopy()
		updated.Spec = tmpl.Spec
		updated.Labels = tmpl.Labels
		updated.Annotations = tmpl.Annotations
		updated.ResourceVersion = tmpl.ResourceVersion
		if err := s.client.Update(ctx, updated); err != nil {
			if k8serrors.IsConflict(err) {
				return nil, domain.NewConflict(fmt.Sprintf("sandbox template %q resourceVersion conflict, please refresh and retry", tmpl.Name))
			}
			if k8serrors.IsNotFound(err) {
				return nil, domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", tmpl.Name))
			}
			return nil, domain.NewInternal(err.Error(), err)
		}
		result := templateFromCRD(ctx, updated)
		return &result, nil
	}

	// Retry-on-conflict path: no resourceVersion supplied (e.g. ws-proxy broadcast).
	var result domain.SandboxTemplate
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		base := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: tmpl.Name}, base); err != nil {
			return err
		}

		// Re-check version ordering on each attempt so we always compare against
		// the freshly fetched resourceVersion.
		if newVer != "" && base.Spec.Version != "" {
			cmp, cmpErr := compareSemver(newVer, base.Spec.Version)
			if cmpErr == nil && cmp <= 0 {
				return fmt.Errorf("version %q must be greater than current version %q: %w",
					newVer, base.Spec.Version, errVersionNotIncreasing)
			}
		}

		updated := base.DeepCopy()
		updated.Spec = tmpl.Spec
		updated.Labels = tmpl.Labels
		updated.Annotations = tmpl.Annotations

		patch := client.MergeFrom(base)
		if err := s.client.Patch(ctx, updated, patch); err != nil {
			return err
		}
		result = templateFromCRD(ctx, updated)
		return nil
	})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", tmpl.Name))
		}
		if isVersionNotIncreasing(err) {
			msg := strings.TrimSuffix(err.Error(), ": "+errVersionNotIncreasing.Error())
			return nil, domain.NewBadRequest(msg)
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return &result, nil
}

func (s *k8sSandboxTemplateService) Delete(ctx context.Context, name string) *domain.AppError {
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: name}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", name))
		}
		return domain.NewInternal(err.Error(), err)
	}
	if err := s.client.Delete(ctx, tmpl); err != nil {
		return domain.NewInternal(fmt.Sprintf("failed to delete sandbox template: %v", err), err)
	}
	return nil
}

func (s *k8sSandboxTemplateService) CreateOrUpdate(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) *domain.AppError {
	// Fast path: try to create first. If the template does not exist this is the
	// common case and avoids an extra Get round-trip.
	createCandidate := tmpl.DeepCopy()
	createCandidate.ResourceVersion = ""
	if createErr := s.client.Create(ctx, createCandidate); createErr == nil {
		return nil
	} else if !k8serrors.IsAlreadyExists(createErr) {
		return domain.NewInternal(createErr.Error(), createErr)
	}

	// Template already exists — patch spec and metadata inside a retry loop.
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: tmpl.Name}, existing); err != nil {
			return err
		}

		updated := existing.DeepCopy()
		updated.Spec = tmpl.Spec
		updated.Labels = tmpl.Labels
		updated.Annotations = tmpl.Annotations
		return s.client.Update(ctx, updated)
	})
	if retryErr != nil {
		if k8serrors.IsNotFound(retryErr) {
			// Deleted between the AlreadyExists check and our Get — retry from scratch.
			return s.CreateOrUpdate(ctx, tmpl)
		}
		return domain.NewInternal(retryErr.Error(), retryErr)
	}
	return nil
}

func (s *k8sSandboxTemplateService) StripStaleGlobalLabels(ctx context.Context, knownNames map[string]struct{}) *domain.AppError {
	list := &agentsv1alpha1.SandboxTemplateList{}
	if err := s.client.List(ctx, list, client.MatchingLabels{agentsv1alpha1.LabelSyncSource: agentsv1alpha1.LabelSyncSourceGlobal}); err != nil {
		return domain.NewInternal(err.Error(), err)
	}
	for i := range list.Items {
		tmpl := &list.Items[i]
		if _, ok := knownNames[tmpl.Name]; ok {
			continue
		}
		// This template carries sync-source=global but was not included in the
		// master's snapshot — remove the stale label so it is treated as local.
		patched := tmpl.DeepCopy()
		delete(patched.Labels, agentsv1alpha1.LabelSyncSource)
		if err := s.client.Patch(ctx, patched, client.MergeFrom(tmpl)); err != nil {
			return domain.NewInternal(fmt.Sprintf("strip stale global label from %q: %v", tmpl.Name, err), err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

func templateFromCRD(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) domain.SandboxTemplate {
	dt := domain.SandboxTemplate{
		Name:        tmpl.Name,
		Version:     tmpl.Spec.Version,
		Description: tmpl.Spec.Description,
		SyncSource:  tmpl.Labels["agentbox.io/sync-source"],
		Docs:        tmpl.Annotations[agentsv1alpha1.SandboxTemplateDocsAnnotationKey],
		CreatedAt:   tmpl.CreationTimestamp.Format(time.RFC3339),
	}
	for _, r := range tmpl.Spec.Runtimes {
		dt.RuntimeNames = append(dt.RuntimeNames, r.Name)
	}
	if tmpl.Spec.Template != nil {
		cpu, memory, err := utilresource.SumContainerResources(tmpl.Spec.Template)
		if err != nil {
			log.FromContext(ctx).V(1).Info("failed to compute template resources", "template", tmpl.Name, "error", err)
		} else {
			dt.CPU = cpu.String()
			dt.Memory = memory.String()
		}
	}
	stripped := tmpl.DeepCopy()
	stripped.ManagedFields = nil
	if b, err := yaml.Marshal(stripped); err == nil {
		dt.CrdYaml = string(b)
	}
	return dt
}

// isVisible reports whether the caller (auth) satisfies the template's visibility rules.
//
// Logic:
//   - visibility == nil || len(rules) == 0 → public, always true
//   - For each rule: teamMatch AND usersMatch → true (OR across rules)
//   - No rule matches → false
func isVisible(visibility *agentsv1alpha1.TemplateVisibility, auth domain.AuthInfo) bool {
	if visibility == nil || len(visibility.Rules) == 0 {
		return true
	}
	for _, rule := range visibility.Rules {
		teamMatch := rule.Team == "" || rule.Team == auth.Team
		usersMatch := len(rule.Users) == 0 || slices.Contains(rule.Users, auth.User)
		if teamMatch && usersMatch {
			return true
		}
	}
	return false
}

// validateSemver validates "x.y.z" format (digits only, no "v" prefix).
func validateSemver(v string) error {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return fmt.Errorf("expected x.y.z, got %q", v)
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return fmt.Errorf("non-numeric component %q", p)
		}
	}
	return nil
}

// compareSemver returns 1 if a > b, 0 if equal, -1 if a < b.
func compareSemver(a, b string) (int, error) {
	parse := func(s string) ([3]int, error) {
		parts := strings.Split(s, ".")
		if len(parts) != 3 {
			return [3]int{}, fmt.Errorf("invalid semver %q", s)
		}
		var r [3]int
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return r, err
			}
			r[i] = n
		}
		return r, nil
	}
	av, err := parse(a)
	if err != nil {
		return 0, err
	}
	bv, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range 3 {
		if av[i] > bv[i] {
			return 1, nil
		}
		if av[i] < bv[i] {
			return -1, nil
		}
	}
	return 0, nil
}
