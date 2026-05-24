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
	"maps"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
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
//
// Template / overrides reconciliation moved off this interface: Pool now stores
// the rendered result only. The Env Reconciler renders new members from the
// source SandboxTemplate at create time, and the Env-level
// `POST /envs/{name}/sync-template` endpoint re-renders existing members. See
// pkg/controllers/sandboxenv/poolsync.go.
type SandboxPoolService interface {
	Create(ctx context.Context, input CreateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError)
	List(ctx context.Context, namespace, team, user string) ([]gen.SandboxPool, *domain.AppError)
	Get(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError)
	Update(ctx context.Context, input UpdateSandboxPoolInput) (*gen.SandboxPool, *domain.AppError)
	Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxPoolResult, *domain.AppError)
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
		opts := renderOptionsFromGen(input.Overrides)
		if !opts.Empty() {
			if err := sandboxrender.Apply(&input.Spec.EmbeddedSandboxTemplate, opts); err != nil {
				return nil, domain.NewBadRequest(err.Error())
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
		if input.PodCreationImagePolicy != nil {
			p.Spec.PodCreationImagePolicy = *input.PodCreationImagePolicy
		}
		if input.OverrideImage != "" {
			if p.Spec.Template == nil || len(p.Spec.Template.Spec.Containers) == 0 {
				return domain.NewBadRequest("image override requires at least one container in the template")
			}
			p.Spec.Template.Spec.Containers[0].Image = input.OverrideImage
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
// fields the API documents (replicas, default timeouts, template reference,
// computed CPU/Memory, SpecYaml for diff) into gen.SandboxPool.
func poolToGen(ctx context.Context, pool *agentsv1alpha1.SandboxPool, tmpl *agentsv1alpha1.SandboxTemplate) gen.SandboxPool {
	spec := gen.SandboxPoolSpec{
		Replicas: pool.Spec.Replicas,
	}
	if pool.Spec.TemplateName != "" {
		spec.TemplateName = ptr.To(pool.Spec.TemplateName)
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
		PendingRequests:         ptr.To(pool.Status.PendingRequests),
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
	// Pool no longer caches overrides — SandboxEnv.spec.overrides is the
	// source of truth and Pool.spec.embedded already reflects the rendered
	// result. result.Overrides stays nil; clients should read overrides
	// from the owning Env.
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
	// Surface the Pool's owning SandboxEnv (Phase 1 adoption stamps a non-
	// controlling OwnerReference). Used by the dashboard for reverse navigation
	// from Pool → Env.
	for i := range pool.OwnerReferences {
		ref := &pool.OwnerReferences[i]
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind &&
			strings.HasPrefix(ref.APIVersion, agentsv1alpha1.GroupVersion.Group+"/") {
			result.OwningEnv = ptr.To(ref.Name)
			break
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

	return nil
}
