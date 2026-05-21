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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	"github.com/scitix/agent-sandbox/pkg/utils/dockerconfig"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// imagePullSecretNamePrefix is the fixed prefix for image-pull Secrets created alongside a pool.
// The full name is "ips-{poolName}" — deterministic so it can be wired into PodSpec
// before the Secret exists.
const imagePullSecretNamePrefix = "ips-"

// imagePullSecretName returns the deterministic Secret name for a given pool.
func imagePullSecretName(poolName string) string {
	return imagePullSecretNamePrefix + poolName
}

// SandboxPoolService defines business operations for SandboxPools.
type SandboxPoolService interface {
	Create(ctx context.Context, input CreateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError)
	List(ctx context.Context, namespace, team, user string) ([]gen.SandboxPool, *domain.AppError)
	Get(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError)
	Update(ctx context.Context, input UpdateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError)
	Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxPoolResult, *domain.AppError)
	// SyncTemplate re-reads the pool's source SandboxTemplate and patches the pool's EmbeddedSandboxTemplate.
	// Does not change replicas. Returns error if pool has no templateName annotation.
	SyncTemplate(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError)
	// SyncTemplatePreview dry-runs SyncTemplate: returns what the EmbeddedSandboxTemplate would look like
	// after applying all overrides, without writing to Kubernetes.
	SyncTemplatePreview(ctx context.Context, namespace, name string) (*gen.SyncTemplatePreviewResult, *domain.AppError)
}

type k8sSandboxPoolService struct {
	client        client.Client
	clientset     kubernetes.Interface   // nil means Event-based diagnostics disabled for Get
	pluginManager *plugins.PluginManager // nil means no plugins (open-source mode)
}

// NewSandboxPoolService creates a new SandboxPoolService backed by the given K8s client.
// clientset may be nil (disables Event-based diagnostics in Get).
// pluginManager may be nil (disables lifecycle plugins — open-source mode).
func NewSandboxPoolService(c client.Client, clientset kubernetes.Interface, pluginManager *plugins.PluginManager) SandboxPoolService {
	return &k8sSandboxPoolService{
		client:        c,
		clientset:     clientset,
		pluginManager: pluginManager,
	}
}

func (s *k8sSandboxPoolService) Create(ctx context.Context, input CreateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError) {
	// 0. Pre-check: verify name does not already exist to avoid orphan side-effects (e.g. reservations)
	existing := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: input.Name, Namespace: input.Namespace}, existing); err == nil {
		return nil, domain.NewConflict(fmt.Sprintf("pool %q already exists", input.Name))
	} else if !k8serrors.IsNotFound(err) {
		return nil, domain.NewInternal(fmt.Sprintf("failed to check pool existence: %v", err), err)
	}

	// If TemplateName is set, fetch the template and record its name and version in annotations for later reference.
	if input.TemplateName != "" {
		tmpl := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: input.TemplateName}, tmpl); err != nil {
			if k8serrors.IsNotFound(err) {
				appErr := domain.NewNotFound(fmt.Sprintf("sandbox template %q not found", input.TemplateName))
				appErr.Detail = s.buildAvailableTemplatesDetail(ctx, input.Team, input.User)
				return nil, appErr
			}
			return nil, domain.NewInternal(fmt.Sprintf("failed to get sandbox template: %v", err), err)
		}
		// Record template source annotations
		if input.Annotations == nil {
			input.Annotations = make(map[string]string)
		}
		if input.Labels == nil {
			input.Labels = make(map[string]string)
		}
		input.Spec.EmbeddedSandboxTemplate = tmpl.Spec.EmbeddedSandboxTemplate
		// Apply caller-supplied overrides on top of the copied template.
		overrides := overridesFromGen(input.Overrides)
		if overrides != nil {
			if appErr := applyPoolTemplateOverrides(&input.Spec.EmbeddedSandboxTemplate, overrides); appErr != nil {
				return nil, appErr
			}
		}
		input.Spec.TemplateName = tmpl.Name
		// Sync most Template labels and annotations to the Pool so that scheduling
		// labels (e.g. scheduling.navix.sh/team) and other metadata are inherited.
		// Labels: agentbox.io/sync-source is excluded — Pools have their own origin
		// semantics and must not inherit the Template's global-sync marker.
		// Annotations: all template annotations are merged first; system-managed keys
		// are overwritten afterwards so they cannot be stomped by template values.
		sandboxpool.SyncLabelsFromTemplate(input.Labels, tmpl.Labels)
		sandboxpool.SyncAnnotationsFromTemplate(input.Annotations, tmpl.Annotations)
		// System-managed keys are set last to ensure correct values regardless of
		// what the Template's own annotations contain.
		input.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey] = tmpl.Name
		input.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = tmpl.Spec.Version
		persistPoolTemplateOverridesInAnnotations(input.Annotations, overrides)
	} else if input.Spec.Template == nil {
		return nil, domain.NewBadRequest("either templateName or spec.template is required")
	}

	// Build the K8s pool object from the (possibly template-resolved) input.
	pool := buildPoolFromInput(input)

	// If an image pull secret is requested, pre-populate pool.Spec.Template.Spec.ImagePullSecrets
	// with the deterministic Secret name BEFORE the pool is persisted. This avoids a second
	// Update and an intermediate window where reconciler-created pods would lack the reference.
	if input.ImagePullSecret != nil && len(input.ImagePullSecret.Registries) > 0 {
		if pool.Spec.Template == nil {
			return nil, domain.NewBadRequest("imagePullSecret requires spec.template (directly or via templateName)")
		}
		secretName := imagePullSecretName(pool.Name)
		refs := pool.Spec.Template.Spec.ImagePullSecrets
		alreadyReferenced := false
		for _, r := range refs {
			if r.Name == secretName {
				alreadyReferenced = true
				break
			}
		}
		if !alreadyReferenced {
			pool.Spec.Template.Spec.ImagePullSecrets = append(refs, corev1.LocalObjectReference{Name: secretName})
		}
		// Persist so SyncTemplate can re-inject the reference on top of newer template revisions.
		if pool.Annotations == nil {
			pool.Annotations = make(map[string]string)
		}
		existing := mustPoolTemplateOverridesFromAnnotations(pool.Annotations)
		if existing == nil {
			existing = &PoolTemplateOverrides{}
		}
		existing.ImagePullSecretName = secretName
		persistPoolTemplateOverridesInAnnotations(pool.Annotations, existing)
	}

	// Validate cross-field constraints (autoscaling bounds, replicas range, etc.)
	if appErr := validatePoolSpec(&pool.Spec); appErr != nil {
		return nil, appErr
	}

	// Run PreCreate plugins (admission control, label injection, reservation, etc.)
	if err := s.pluginManager.PreCreatePool(ctx, pool); err != nil {
		return nil, err
	}

	// Create K8s resource
	if err := s.client.Create(ctx, pool); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			log.FromContext(ctx).Error(err, "pool already exists after pre-check passed; plugin side-effects may need manual cleanup", "name", pool.Name)
			return nil, domain.NewConflict("pool already exists")
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Materialise the image pull Secret immediately after the pool, with an OwnerReference
	// to the pool so it is garbage-collected automatically on pool deletion. Any failure
	// here deletes the pool to prevent pods referencing a non-existent Secret.
	if input.ImagePullSecret != nil && len(input.ImagePullSecret.Registries) > 0 {
		if appErr := s.createImagePullSecret(ctx, pool, input.ImagePullSecret); appErr != nil {
			if delErr := s.client.Delete(ctx, pool); delErr != nil && !k8serrors.IsNotFound(delErr) {
				log.FromContext(ctx).Error(delErr, "rollback: failed to delete pool after image pull secret creation failed", "pool", pool.Name)
			}
			return nil, appErr
		}
	}

	result := poolToGen(ctx, pool, nil)
	return &result, nil
}

// createImagePullSecret materialises the given credentials as a kubernetes.io/dockerconfigjson
// Secret named ips-{poolName}, with OwnerReference pointing at the pool.
func (s *k8sSandboxPoolService) createImagePullSecret(ctx context.Context, pool *agentsv1alpha1.SandboxPool, input *gen.ImagePullSecretInput) *domain.AppError {
	creds := make([]dockerconfig.RegistryCredential, 0, len(input.Registries))
	for _, r := range input.Registries {
		creds = append(creds, dockerconfig.RegistryCredential{
			Registry: r.Registry,
			Username: r.Username,
			Password: r.Password,
		})
	}
	payload, err := dockerconfig.Build(creds)
	if err != nil {
		return domain.NewBadRequest(fmt.Sprintf("invalid imagePullSecret: %v", err))
	}
	blockOwnerDeletion := true
	isController := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imagePullSecretName(pool.Name),
			Namespace: pool.Namespace,
			Labels: map[string]string{
				"agentbox.io/type": "image-pull-secret",
				"agentbox.io/pool": pool.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         agentsv1alpha1.GroupVersion.String(),
				Kind:               "SandboxPool",
				Name:               pool.Name,
				UID:                pool.UID,
				BlockOwnerDeletion: &blockOwnerDeletion,
				Controller:         &isController,
			}},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: payload,
		},
	}
	if err := s.client.Create(ctx, secret); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return domain.NewConflict(fmt.Sprintf("image pull secret %q already exists", secret.Name))
		}
		return domain.NewInternal(fmt.Sprintf("failed to create image pull secret: %v", err), err)
	}
	return nil
}

// buildAvailableTemplatesDetail lists templates visible to the calling
// team/user and returns a compact summary for attaching to 404 errors when a
// requested template does not exist. Best-effort: list errors return nil so
// the 404 path is never worsened by enrichment.
func (s *k8sSandboxPoolService) buildAvailableTemplatesDetail(ctx context.Context, team, user string) *domain.AvailableTemplatesDetail {
	list := &agentsv1alpha1.SandboxTemplateList{}
	if err := s.client.List(ctx, list); err != nil {
		log.FromContext(ctx).V(1).Info("buildAvailableTemplatesDetail: list failed", "err", err)
		return nil
	}
	auth := domain.AuthInfo{Team: team, User: user}
	summaries := make([]domain.AvailableTemplateSummary, 0, len(list.Items))
	for i := range list.Items {
		t := &list.Items[i]
		if !isVisible(t.Spec.Visibility, auth) {
			continue
		}
		summaries = append(summaries, domain.AvailableTemplateSummary{
			Name:        t.Name,
			Description: t.Spec.Description,
			SyncSource:  t.Labels[agentsv1alpha1.LabelSyncSource],
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	hint := "Template not found. Pick a name from availableTemplates, or omit templateName to define the pool inline."
	if len(summaries) == 0 {
		hint = "No templates are visible to this team/user. Ask an admin to create/publish one, or omit templateName to define the pool inline."
	}
	return &domain.AvailableTemplatesDetail{
		AvailableTemplates: summaries,
		Hint:               hint,
	}
}

// List returns all SandboxPools in the given namespace, with per-pod diagnostics
// derived from Pod YAML only (no Kubernetes Events API calls).
// When team and user are non-empty, only pools with matching labels are returned.
func (s *k8sSandboxPoolService) List(ctx context.Context, namespace, team, user string) ([]gen.SandboxPool, *domain.AppError) {
	listOpts := []client.ListOption{client.InNamespace(namespace)}
	if team != "" && user != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	poolList := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, poolList, listOpts...); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	items := make([]gen.SandboxPool, 0, len(poolList.Items))
	for i := range poolList.Items {
		p := &poolList.Items[i]
		items = append(items, poolToGen(ctx, p, nil))
	}
	// Sort by name for consistent ordering (especially important for tests)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// Get returns a single SandboxPool.
func (s *k8sSandboxPoolService) Get(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	var tmpl *agentsv1alpha1.SandboxTemplate
	if templateName := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]; templateName != "" {
		t := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, t); err == nil {
			tmpl = t
		}
	}

	result := poolToGen(ctx, pool, tmpl)
	return &result, nil
}

func (s *k8sSandboxPoolService) Update(ctx context.Context, input UpdateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError) {
	key := client.ObjectKey{Namespace: input.Namespace, Name: input.Name}

	if input.OverrideImage != "" {
		if err := ValidateContainerImage(input.OverrideImage); err != nil {
			return nil, domain.NewBadRequest(err.Error())
		}
	}

	// applyInputToPool stamps the caller-supplied changes onto a pool object.
	applyInput := func(p *agentsv1alpha1.SandboxPool) *domain.AppError {
		if input.Replicas != nil {
			p.Spec.Replicas = *input.Replicas
		}
		if input.MinReplicas != nil {
			p.Spec.MinReplicas = input.MinReplicas
		}
		if input.MaxReplicas != nil {
			p.Spec.MaxReplicas = input.MaxReplicas
		}
		if input.PodCreationImagePolicy != nil {
			p.Spec.PodCreationImagePolicy = *input.PodCreationImagePolicy
		}
		if input.OverrideImage != "" {
			if p.Annotations == nil {
				p.Annotations = make(map[string]string)
			}
			if p.Spec.Template == nil || len(p.Spec.Template.Spec.Containers) == 0 {
				return domain.NewBadRequest("image override requires at least one container in the template")
			}
			p.Spec.Template.Spec.Containers[0].Image = input.OverrideImage
			existing := mustPoolTemplateOverridesFromAnnotations(p.Annotations)
			if existing == nil {
				existing = &PoolTemplateOverrides{}
			}
			existing.Image = input.OverrideImage
			persistPoolTemplateOverridesInAnnotations(p.Annotations, existing)
		}
		if input.Autoscaling != nil {
			p.Spec.Autoscaling = input.Autoscaling
			if !input.Autoscaling.Enabled {
				p.Spec.MinReplicas = nil
				p.Spec.MaxReplicas = nil
			}
		}
		return validatePoolSpec(&p.Spec)
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, key, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", input.Name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	if appErr := applyInput(pool); appErr != nil {
		return nil, appErr
	}

	pods, err := indexer.ListPodsBySandboxPool(ctx, s.client, pool.Namespace, pool.Name)
	if err != nil {
		return nil, domain.NewInternal("failed to list pods for pool", err)
	}

	updated, pluginErr := s.pluginManager.PreUpdatePool(ctx, pool, pods)
	if pluginErr != nil {
		return nil, pluginErr
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}

		if appErr := applyInput(current); appErr != nil {
			return fmt.Errorf("%s: %w", appErr.Error(), errBadRequest)
		}

		if updated {
			if _, pluginErr := s.pluginManager.PreUpdatePool(ctx, current, pods); pluginErr != nil {
				return pluginErr
			}
		}

		pool = current
		return s.client.Update(ctx, current)
	})

	if retryErr != nil {
		if k8serrors.IsNotFound(retryErr) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", input.Name))
		}
		if isBadRequest(retryErr) {
			return nil, domain.NewBadRequest(strings.TrimSuffix(retryErr.Error(), ": "+errBadRequest.Error()))
		}
		return nil, domain.NewInternal(retryErr.Error(), retryErr)
	}

	result := poolToGen(ctx, pool, nil)
	return &result, nil
}

func (s *k8sSandboxPoolService) SyncTemplate(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError) {
	// Resolve the template name from the pool once upfront — if the pool doesn't
	// exist or has no templateName annotation we can fail fast before any retries.
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	templateName := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]
	if templateName == "" {
		return nil, domain.NewBadRequest("pool has no associated template (templateName annotation missing)")
	}

	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("source template %q not found", templateName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	var result gen.SandboxPool
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, current); err != nil {
			return err
		}

		updated := current.DeepCopy()
		updated.Spec.EmbeddedSandboxTemplate = tmpl.Spec.EmbeddedSandboxTemplate
		if updated.Labels == nil {
			updated.Labels = make(map[string]string)
		}
		if updated.Annotations == nil {
			updated.Annotations = make(map[string]string)
		}
		// Sync most Template labels and annotations to keep the Pool in sync with the
		// Template's metadata. Same exclusion rules as at creation time apply here:
		// agentbox.io/sync-source is excluded from labels, and system-managed annotation
		// keys are overwritten after the merge to ensure correct values.
		sandboxpool.SyncLabelsFromTemplate(updated.Labels, tmpl.Labels)
		sandboxpool.SyncAnnotationsFromTemplate(updated.Annotations, tmpl.Annotations)
		overrides, appErr := poolTemplateOverridesFromAnnotations(updated.Annotations)
		if appErr != nil {
			return fmt.Errorf("%s: %w", appErr.Error(), errBadRequest)
		}
		if appErr := applyPoolTemplateOverrides(&updated.Spec.EmbeddedSandboxTemplate, overrides); appErr != nil {
			return fmt.Errorf("%s: %w", appErr.Error(), errBadRequest)
		}
		persistPoolTemplateOverridesInAnnotations(updated.Annotations, overrides)
		updated.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey] = tmpl.Name
		updated.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = tmpl.Spec.Version

		patch := client.MergeFrom(current)
		if err := s.client.Patch(ctx, updated, patch); err != nil {
			return err
		}
		result = poolToGen(ctx, updated, nil)
		return nil
	})
	if retryErr != nil {
		if k8serrors.IsNotFound(retryErr) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		if isBadRequest(retryErr) {
			return nil, domain.NewBadRequest(strings.TrimSuffix(retryErr.Error(), ": "+errBadRequest.Error()))
		}
		return nil, domain.NewInternal(retryErr.Error(), retryErr)
	}

	return &result, nil
}

func (s *k8sSandboxPoolService) SyncTemplatePreview(ctx context.Context, namespace, name string) (*gen.SyncTemplatePreviewResult, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	templateName := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]
	if templateName == "" {
		return nil, domain.NewBadRequest("pool has no associated template (templateName annotation missing)")
	}

	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("source template %q not found", templateName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Build a scratch pool copy so we can reuse the same override helpers without touching the real pool.
	scratch := pool.DeepCopy()
	scratch.Spec.EmbeddedSandboxTemplate = tmpl.Spec.EmbeddedSandboxTemplate
	if scratch.Annotations == nil {
		scratch.Annotations = make(map[string]string)
	}
	sandboxpool.SyncAnnotationsFromTemplate(scratch.Annotations, tmpl.Annotations)

	overrides, appErr := poolTemplateOverridesFromAnnotations(scratch.Annotations)
	if appErr != nil {
		return nil, domain.NewBadRequest(appErr.Error())
	}
	if appErr := applyPoolTemplateOverrides(&scratch.Spec.EmbeddedSandboxTemplate, overrides); appErr != nil {
		return nil, domain.NewBadRequest(appErr.Error())
	}

	b, err := yaml.Marshal(scratch.Spec.EmbeddedSandboxTemplate)
	if err != nil {
		return nil, domain.NewInternal("marshal preview: "+err.Error(), err)
	}

	return &gen.SyncTemplatePreviewResult{
		SpecYaml: string(b),
		Version:  tmpl.Spec.Version,
	}, nil
}

func (s *k8sSandboxPoolService) Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxPoolResult, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Run PreDelete plugins
	if err := s.pluginManager.PreDeletePool(ctx, pool); err != nil {
		return nil, err
	}

	if err := s.client.Delete(ctx, pool); err != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to delete pool: %v", err), err)
	}

	return &gen.DeleteSandboxPoolResult{
		Name:      pool.Name,
		Namespace: pool.Namespace,
	}, nil
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

// errBadRequest is a sentinel wrapped inside RetryOnConflict loops to signal
// a non-retryable business-rule violation (distinct from transient I/O errors).
var errBadRequest = errors.New("bad request")

func isBadRequest(err error) bool {
	return errors.Is(err, errBadRequest)
}

// poolToGen converts a CRD SandboxPool (plus optional source template) to the gen
// wire shape. The CRD spec is intentionally not exposed; instead we project the
// fields the API documents (replicas, autoscaling, default timeouts, template
// reference, computed CPU/Memory, SpecYaml for diff) into gen.SandboxPool.
func poolToGen(ctx context.Context, pool *agentsv1alpha1.SandboxPool, tmpl *agentsv1alpha1.SandboxTemplate) gen.SandboxPool {
	spec := gen.SandboxPoolSpec{
		Replicas: pool.Spec.Replicas,
	}
	if pool.Spec.MinReplicas != nil {
		spec.MinReplicas = pool.Spec.MinReplicas
	}
	if pool.Spec.MaxReplicas != nil {
		spec.MaxReplicas = pool.Spec.MaxReplicas
	}
	if pool.Spec.TemplateName != "" {
		spec.TemplateName = ptr.To(pool.Spec.TemplateName)
	}
	if pool.Spec.Autoscaling != nil {
		spec.Autoscaling = autoscalingToGen(pool.Spec.Autoscaling)
	}
	if pool.Spec.DefaultStartupTimeout != nil {
		spec.DefaultStartupTimeout = ptr.To(pool.Spec.DefaultStartupTimeout.Duration.String())
	}
	if pool.Spec.DefaultIdleTimeout != nil {
		spec.DefaultIdleTimeout = ptr.To(pool.Spec.DefaultIdleTimeout.Duration.String())
	}
	if pool.Spec.PodCreationImagePolicy != "" {
		policy := gen.SandboxPoolSpecPodCreationImagePolicy(pool.Spec.PodCreationImagePolicy)
		spec.PodCreationImagePolicy = &policy
	}

	status := gen.SandboxPoolStatus{
		IdleReplicas:            ptr.To(pool.Status.IdleReplicas),
		UnavailableIdleReplicas: ptr.To(pool.Status.UnavailableIdleReplicas),
		RunningReplicas:         ptr.To(pool.Status.RunningReplicas),
		StartingReplicas:        ptr.To(pool.Status.StartingReplicas),
		StoppingReplicas:        ptr.To(pool.Status.StoppingReplicas),
		FailedReplicas:          ptr.To(pool.Status.FailedReplicas),
	}
	if pool.Status.Phase != "" {
		phase := gen.SandboxPoolStatusPhase(pool.Status.Phase)
		status.Phase = &phase
	}

	specYaml := embeddedTemplateToYAML(pool.Spec.EmbeddedSandboxTemplate)
	createdAt := pool.CreationTimestamp.UTC()
	result := gen.SandboxPool{
		Name:            pool.Name,
		Namespace:       pool.Namespace,
		Spec:            spec,
		Status:          status,
		Team:            ptr.To(pool.Labels[agentsv1alpha1.LabelTeam]),
		User:            ptr.To(pool.Labels[agentsv1alpha1.LabelUser]),
		TemplateVersion: ptr.To(pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]),
		SpecYaml:        ptr.To(specYaml),
		CreatedAt:       &createdAt,
	}
	if overrides := mustPoolTemplateOverridesFromAnnotations(pool.Annotations); overrides != nil {
		result.Overrides = &gen.PoolTemplateOverrides{}
		if overrides.Image != "" {
			result.Overrides.Image = &overrides.Image
		}
		if overrides.ResourceMultiplier > 1 {
			result.Overrides.ResourceMultiplier = &overrides.ResourceMultiplier
		}
	}
	if tmpl != nil {
		if v := tmpl.Annotations[agentsv1alpha1.SandboxTemplateDocsAnnotationKey]; v != "" {
			result.PoolDocs = ptr.To(v)
		}
	}
	if pool.Spec.Template != nil {
		cpu, memory, err := utilresource.SumContainerResources(pool.Spec.Template)
		if err != nil {
			log.FromContext(ctx).V(1).Info("failed to compute pool resources", "pool", pool.Name, "error", err)
		} else {
			result.Cpu = ptr.To(cpu.String())
			result.Memory = ptr.To(memory.String())
		}
	}
	return result
}

// autoscalingToGen converts a CRD PoolAutoscalingSpec to the generated gen type.
func autoscalingToGen(a *agentsv1alpha1.PoolAutoscalingSpec) *gen.PoolAutoscalingSpec {
	if a == nil {
		return nil
	}
	result := &gen.PoolAutoscalingSpec{
		Enabled: &a.Enabled,
	}
	if a.ScaleUpPolicy != nil {
		mode := gen.PoolScaleUpPolicyMode(a.ScaleUpPolicy.Mode)
		result.ScaleUpPolicy = &gen.PoolScaleUpPolicy{
			Mode:                 &mode,
			CooldownSeconds:      &a.ScaleUpPolicy.CooldownSeconds,
			IdleThresholdSeconds: &a.ScaleUpPolicy.IdleThresholdSeconds,
		}
	}
	if a.ScaleDownPolicy != nil {
		result.ScaleDownPolicy = &gen.PoolScaleDownPolicy{
			IdleTimeoutSeconds:      &a.ScaleDownPolicy.IdleTimeoutSeconds,
			StabilizationSeconds:    &a.ScaleDownPolicy.StabilizationSeconds,
			ProtectionWindowSeconds: &a.ScaleDownPolicy.ProtectionWindowSeconds,
		}
	}
	return result
}

// embeddedTemplateToYAML serializes the EmbeddedSandboxTemplate fields (idleImage, runtimes,
// reservation, template) to a YAML string for use in the SyncTemplate diff view.
// Returns an empty string if marshalling fails.
func embeddedTemplateToYAML(emb agentsv1alpha1.EmbeddedSandboxTemplate) string {
	type diffable struct {
		IdleImage string                              `json:"idleImage,omitempty"`
		Runtimes  []agentsv1alpha1.SandboxRuntimeSpec `json:"runtimes,omitempty"`
		Template  *corev1.PodTemplateSpec             `json:"template,omitempty"`
	}
	d := diffable{
		IdleImage: emb.IdleImage,
		Runtimes:  emb.Runtimes,
		Template:  emb.Template,
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildPoolFromInput constructs a minimal SandboxPool from the given input.
func buildPoolFromInput(input CreateSandboxPoolInput) *agentsv1alpha1.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{}
	pool.Name = input.Name
	pool.Namespace = input.Namespace
	pool.Annotations = input.Annotations
	pool.Spec = input.Spec

	// Merge caller-supplied labels and always stamp team/user so that
	// SandboxPoolService.List can filter by MatchingLabels(team, user).
	labels := make(map[string]string, len(input.Labels)+2)
	maps.Copy(labels, input.Labels)
	if input.Team != "" {
		labels[agentsv1alpha1.LabelTeam] = input.Team
	}
	if input.User != "" {
		labels[agentsv1alpha1.LabelUser] = input.User
	}
	if len(labels) > 0 {
		pool.Labels = labels
	}
	return pool
}

// validatePoolSpec checks cross-field constraints on the pool spec.
// Call after all input fields have been applied (Create or Update).
func validatePoolSpec(spec *agentsv1alpha1.SandboxPoolSpec) *domain.AppError {
	// IdleImage must be set and must differ from the target container image.
	// A pod in idle state runs idleImage; a running sandbox runs the container image.
	// If they were the same, IsInplaceUpdateCompleted could not distinguish between
	// "sandbox started" and "pod reverted to idle".
	if spec.IdleImage == "" {
		return domain.NewBadRequest("idleImage is required")
	}
	if spec.Template != nil && len(spec.Template.Spec.Containers) > 0 {
		if spec.IdleImage == spec.Template.Spec.Containers[0].Image {
			return domain.NewBadRequest(fmt.Sprintf("idleImage (%q) must differ from the container image (%q)", spec.IdleImage, spec.Template.Spec.Containers[0].Image))
		}
	}

	if spec.Autoscaling != nil && spec.Autoscaling.Enabled {
		if spec.MinReplicas == nil {
			return domain.NewBadRequest("autoscaling requires minReplicas to be set")
		}
		if spec.MaxReplicas == nil {
			return domain.NewBadRequest("autoscaling requires maxReplicas to be set")
		}
		minVal := *spec.MinReplicas
		maxVal := *spec.MaxReplicas
		if minVal < 0 {
			return domain.NewBadRequest("minReplicas must be >= 0")
		}
		if maxVal < 0 {
			return domain.NewBadRequest("maxReplicas must be >= 0")
		}
		if minVal > maxVal {
			return domain.NewBadRequest(fmt.Sprintf("minReplicas (%d) must be <= maxReplicas (%d)", minVal, maxVal))
		}
		if spec.Replicas < minVal {
			return domain.NewBadRequest(fmt.Sprintf("replicas (%d) must be >= minReplicas (%d)", spec.Replicas, minVal))
		}
		if spec.Replicas > maxVal {
			return domain.NewBadRequest(fmt.Sprintf("replicas (%d) must be <= maxReplicas (%d)", spec.Replicas, maxVal))
		}
	}
	return nil
}

// applyPoolTemplateOverrides mutates tmpl in-place after it has been deep-copied from the
// source SandboxTemplate. The CRD stores the final computed values; the override params
// themselves are not persisted.
func applyPoolTemplateOverrides(
	tmpl *agentsv1alpha1.EmbeddedSandboxTemplate,
	overrides *PoolTemplateOverrides,
) *domain.AppError {
	if overrides == nil {
		return nil
	}
	if overrides.Image != "" {
		if err := ValidateContainerImage(overrides.Image); err != nil {
			return domain.NewBadRequest(err.Error())
		}
		if tmpl.Template == nil || len(tmpl.Template.Spec.Containers) == 0 {
			return domain.NewBadRequest("image override requires at least one container in the template")
		}
		tmpl.Template.Spec.Containers[0].Image = overrides.Image
	}
	if overrides.ResourceMultiplier > 1 {
		if tmpl.Template == nil || len(tmpl.Template.Spec.Containers) == 0 {
			return domain.NewBadRequest("resourceMultiplier requires at least one container in the template")
		}
		for i := range tmpl.Template.Spec.Containers {
			if err := multiplyContainerResources(&tmpl.Template.Spec.Containers[i], overrides.ResourceMultiplier); err != nil {
				return domain.NewBadRequest(
					fmt.Sprintf("container %q: %v", tmpl.Template.Spec.Containers[i].Name, err),
				)
			}
		}
	}
	if overrides.ImagePullSecretName != "" {
		if tmpl.Template == nil {
			return domain.NewBadRequest("imagePullSecret override requires spec.template in the template")
		}
		alreadyReferenced := false
		for _, r := range tmpl.Template.Spec.ImagePullSecrets {
			if r.Name == overrides.ImagePullSecretName {
				alreadyReferenced = true
				break
			}
		}
		if !alreadyReferenced {
			tmpl.Template.Spec.ImagePullSecrets = append(tmpl.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: overrides.ImagePullSecretName})
		}
	}
	return nil
}

func persistPoolTemplateOverridesInAnnotations(annotations map[string]string, overrides *PoolTemplateOverrides) {
	if annotations == nil {
		return
	}
	delete(annotations, agentsv1alpha1.SandboxPoolOverridesAnnotationKey)
	if overrides == nil {
		return
	}
	data, err := json.Marshal(overrides)
	if err != nil {
		return
	}
	annotations[agentsv1alpha1.SandboxPoolOverridesAnnotationKey] = string(data)
}

func poolTemplateOverridesFromAnnotations(annotations map[string]string) (*PoolTemplateOverrides, *domain.AppError) {
	raw, ok := annotations[agentsv1alpha1.SandboxPoolOverridesAnnotationKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	overrides := &PoolTemplateOverrides{}
	if err := json.Unmarshal([]byte(raw), overrides); err != nil {
		return nil, domain.NewBadRequest(fmt.Sprintf("invalid pool overrides annotation: %v", err))
	}
	if overrides.Image == "" && overrides.ResourceMultiplier <= 1 && overrides.ImagePullSecretName == "" {
		return nil, nil
	}
	return overrides, nil
}

func mustPoolTemplateOverridesFromAnnotations(annotations map[string]string) *PoolTemplateOverrides {
	overrides, err := poolTemplateOverridesFromAnnotations(annotations)
	if err != nil {
		return nil
	}
	return overrides
}

// multiplyContainerResources scales CPU and Memory requests+limits of a single container
// by the given integer multiplier.
//
// CPU uses MilliValue arithmetic (lossless for all standard Kubernetes CPU quantities):
//
//	"500m" × 2 → NewMilliQuantity(500*2, DecimalSI) → "1"
//	"4"    × 2 → NewMilliQuantity(4000*2, DecimalSI) → "8"
//
// Memory uses Value (byte) arithmetic (always integer, BinarySI normalises units):
//
//	"4Gi"  × 2 → NewQuantity(4294967296*2, BinarySI) → "8Gi"
func multiplyContainerResources(c *corev1.Container, multiplier int32) error {
	m := int64(multiplier)
	_, hasCPUReq := c.Resources.Requests[corev1.ResourceCPU]
	_, hasMemReq := c.Resources.Requests[corev1.ResourceMemory]
	_, hasCPULim := c.Resources.Limits[corev1.ResourceCPU]
	_, hasMemLim := c.Resources.Limits[corev1.ResourceMemory]
	if !hasCPUReq && !hasCPULim {
		return fmt.Errorf("has no CPU requests or limits; cannot apply resourceMultiplier")
	}
	if !hasMemReq && !hasMemLim {
		return fmt.Errorf("has no memory requests or limits; cannot apply resourceMultiplier")
	}
	if c.Resources.Requests == nil {
		c.Resources.Requests = corev1.ResourceList{}
	}
	if c.Resources.Limits == nil {
		c.Resources.Limits = corev1.ResourceList{}
	}
	if cpuReq, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
		c.Resources.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(cpuReq.MilliValue()*m, resource.DecimalSI)
	}
	if memReq, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
		c.Resources.Requests[corev1.ResourceMemory] = *resource.NewQuantity(memReq.Value()*m, resource.BinarySI)
	}
	if cpuLim, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
		c.Resources.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(cpuLim.MilliValue()*m, resource.DecimalSI)
	}
	if memLim, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
		c.Resources.Limits[corev1.ResourceMemory] = *resource.NewQuantity(memLim.Value()*m, resource.BinarySI)
	}
	return nil
}
