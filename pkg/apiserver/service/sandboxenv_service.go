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
	"fmt"
	"maps"
	"sort"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// SandboxEnvService is the business-layer surface for SandboxEnv resources.
//
// Scope (Phase 1):
//   - List / Get exposed for the dashboard
//   - UpdateAutoscaling is the only mutation — Envs are otherwise managed by
//     the Phase 1 PoolAdoptionReconciler (1:1 from existing SandboxPools).
//
// Future mutations (Create / Delete / Edit members) will land here when the
// "Env-creates-Pool" flow ships.
type SandboxEnvService interface {
	// List returns the SandboxEnvs in namespace visible to the caller.
	// When team/user are non-empty the result is filtered by the standard
	// scheduling.navix.sh/{team,user} labels (matching the Pool model).
	List(ctx context.Context, namespace, team, user string) ([]gen.SandboxEnv, *domain.AppError)
	// Get returns a single Env or NotFound.
	Get(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError)
	// Create posts a new SandboxEnv. The Env Reconciler picks up the new
	// object and materialises member SandboxPools from spec.Quotas — this
	// service call does NOT create Pools directly.
	Create(ctx context.Context, input CreateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError)
	// Update merges any subset of editable spec fields into the existing
	// Env, retrying on conflict. Returns the post-write Env.
	Update(ctx context.Context, input UpdateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError)
	// Delete issues a foreground delete on the Env. Member Pools are
	// cascade-deleted via OwnerReferences (controller=true,
	// blockOwnerDeletion=true) once the Env Reconciler stamps the upgraded
	// owner reference shape.
	Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxEnvResult, *domain.AppError)
	// SyncTemplate re-renders every member SandboxPool against the current
	// SandboxTemplate body + the Env's overrides, advancing each Pool's
	// template-version annotation. Use this after an admin edits the
	// underlying Template — Env-level overrides edits propagate
	// automatically via Update().
	SyncTemplate(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError)

	// AddMemberPool appends a new member to the env's local cluster segment.
	// The supplied EnvClusterMember provides one of (InstanceType+Multiplier,
	// InlineResources) plus optional labels (e.g. QuotaURLLabel). The server
	// derives Name and ScalingGroup; any caller-supplied values for these
	// fields are overwritten. The Env Reconciler later materialises the
	// SandboxPool CR. 503 when localClusterID is empty, 409 when the derived
	// name collides with an existing member.
	AddMemberPool(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError)
	// UpdateMemberPool adjusts the named member's replica counts. patch may
	// carry Replicas (only when this member's ScalingGroup has autoscaling
	// disabled) and MaxReplicas. Resource shape, instanceType, labels, and
	// annotations are immutable post-create.
	UpdateMemberPool(ctx context.Context, namespace, envName, poolName, localClusterID string, patch MemberPoolPatch) (*gen.SandboxPool, *domain.AppError)
	// DeleteMemberPool removes the named member from the env's local cluster
	// segment. The Reconciler cascade-deletes the SandboxPool CR.
	DeleteMemberPool(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError)
	// ListMemberPools enumerates SandboxPool CRs owned by envName (matched on
	// OwnerReferences). Projects each via poolToGen for the wire shape.
	ListMemberPools(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError)
	// GetMemberPool fetches one SandboxPool CR and verifies it is owned by
	// envName before projecting it. Returns 404 if the Pool exists but is not
	// owned by envName (so callers cannot probe across envs).
	GetMemberPool(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError)
}

// CreateSandboxEnvInput is the parsed CreateSandboxEnvRequest with auth
// context resolved.
type CreateSandboxEnvInput struct {
	Name      string
	Namespace string
	Team      string // copied from auth, injected as label
	User      string // copied from auth, injected as label

	TemplateRef    agentsv1alpha1.SandboxEnvTemplateRef
	Mode           agentsv1alpha1.SandboxEnvMode
	Members        []agentsv1alpha1.EnvClusterMember
	LocalClusterID string // cluster the supplied members belong to
	Overrides      *agentsv1alpha1.EnvOverridesSpec
	// ImagePullSecret, when non-nil, instructs the service to materialise a
	// dockerconfigjson Secret named ips-{envName} with an OwnerRef pointing
	// at the Env (cascade-delete free). The Env Reconciler stamps a
	// LocalObjectReference to that Secret into every member Pool.
	ImagePullSecret *gen.ImagePullSecretInput

	Labels      map[string]string
	Annotations map[string]string
}

// MemberPoolPatch is the editable subset of EnvClusterMember exposed to
// PUT /v1/envs/{name}/sandboxpools/{poolName}. Pointer fields disambiguate
// "leave unchanged" from "explicit zero".
type MemberPoolPatch struct {
	Replicas    *int32
	MaxReplicas *int32
}

// UpdateSandboxEnvInput carries the editable patch for an Env. Pointer
// fields disambiguate "not specified" from "explicit zero/empty"; passing
// a non-nil pointer means "replace with this value".
type UpdateSandboxEnvInput struct {
	Name      string
	Namespace string

	Autoscaling    *gen.EnvAutoscalingSpec
	Members        *[]agentsv1alpha1.EnvClusterMember
	LocalClusterID string // required when Members is non-nil
	Overrides      *agentsv1alpha1.EnvOverridesSpec
	// ImagePullSecret, when non-nil, upserts the dockerconfigjson Secret
	// backing this Env's image-pull credentials. Nil means leave existing
	// Secret untouched.
	ImagePullSecret *gen.ImagePullSecretInput
}

type k8sSandboxEnvService struct {
	client   client.Client
	admitter PoolAdmitter
	// instProv and quotaProv drive server-side derivation of PoolName +
	// ScalingGroup. Both must be non-nil — pass the open-source Noop when
	// no backend is configured.
	instProv  instancetypeplugin.Provider
	quotaProv quotaplugin.Provider
}

// NewSandboxEnvService constructs the default service implementation backed
// by the K8s client, the supplied PoolAdmitter, and the two providers used
// to derive PoolName + ScalingGroup. A nil admitter is treated as
// NoOpPoolAdmitter; nil providers fall through to their Noop equivalents so
// unit tests can pass nils.
func NewSandboxEnvService(c client.Client, admitter PoolAdmitter, instProv instancetypeplugin.Provider, quotaProv quotaplugin.Provider) SandboxEnvService {
	if admitter == nil {
		admitter = NoOpPoolAdmitter{}
	}
	if instProv == nil {
		instProv = instancetypeplugin.NewNoop()
	}
	if quotaProv == nil {
		quotaProv = quotaplugin.NewNoop()
	}
	return &k8sSandboxEnvService{client: c, admitter: admitter, instProv: instProv, quotaProv: quotaProv}
}

// List enumerates Envs in the namespace, filtered by team/user labels when
// supplied. Sorted by name for deterministic test output.
func (s *k8sSandboxEnvService) List(ctx context.Context, namespace, team, user string) ([]gen.SandboxEnv, *domain.AppError) {
	listOpts := []client.ListOption{client.InNamespace(namespace)}
	if team != "" && user != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	envList := &agentsv1alpha1.SandboxEnvList{}
	if err := s.client.List(ctx, envList, listOpts...); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	items := make([]gen.SandboxEnv, 0, len(envList.Items))
	for i := range envList.Items {
		items = append(items, envToGen(&envList.Items[i]))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// Get fetches one Env by namespaced name.
func (s *k8sSandboxEnvService) Get(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", name, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	result := envToGen(env)
	s.enrichImagePullSecretStatus(ctx, env, &result)
	return &result, nil
}

// enrichImagePullSecretStatus populates result.spec.overrides.imagePullSecretConfigured
// by checking whether the convention-named dockerconfigjson Secret exists.
// Best-effort: API errors fall back to "unknown" (field stays nil).
func (s *k8sSandboxEnvService) enrichImagePullSecretStatus(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	out *gen.SandboxEnv,
) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: env.Namespace,
		Name:      agentsv1alpha1.EnvImagePullSecretName(env.Name),
	}
	if err := s.client.Get(ctx, key, secret); err != nil {
		return
	}
	if out.Spec.Overrides == nil {
		out.Spec.Overrides = &gen.EnvOverrides{}
	}
	out.Spec.Overrides.ImagePullSecretConfigured = ptr.To(true)
}

// Create persists a new SandboxEnv. The Env Reconciler picks up the resulting
// object and materialises member SandboxPools from spec.Quotas.
func (s *k8sSandboxEnvService) Create(ctx context.Context, input CreateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError) {
	existing := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: input.Namespace, Name: input.Name}, existing); err == nil {
		return nil, domain.NewConflict(fmt.Sprintf("sandbox env %q already exists in namespace %s", input.Name, input.Namespace))
	} else if !k8serrors.IsNotFound(err) {
		return nil, domain.NewInternal(err.Error(), err)
	}

	if input.TemplateRef.Name == "" {
		return nil, domain.NewBadRequest("templateRef.name is required")
	}
	mode := input.Mode
	if mode == "" {
		mode = agentsv1alpha1.SandboxEnvModeWarmPool
	}

	env := &agentsv1alpha1.SandboxEnv{}
	env.Name = input.Name
	env.Namespace = input.Namespace

	labels := make(map[string]string, len(input.Labels)+2)
	maps.Copy(labels, input.Labels)
	if input.Team != "" {
		labels[agentsv1alpha1.LabelTeam] = input.Team
	}
	if input.User != "" {
		labels[agentsv1alpha1.LabelUser] = input.User
	}
	if len(labels) > 0 {
		env.Labels = labels
	}
	if len(input.Annotations) > 0 {
		env.Annotations = make(map[string]string, len(input.Annotations))
		maps.Copy(env.Annotations, input.Annotations)
	}

	env.Spec = agentsv1alpha1.SandboxEnvSpec{
		TemplateRef: input.TemplateRef,
		Mode:        mode,
		Overrides:   input.Overrides,
	}
	if len(input.Members) > 0 {
		if input.LocalClusterID == "" {
			return nil, domain.NewBadRequest("localClusterID is required when members is set")
		}
		env.Spec.Clusters = []agentsv1alpha1.EnvClusterSpec{{
			ClusterID: input.LocalClusterID,
			Members:   append([]agentsv1alpha1.EnvClusterMember(nil), input.Members...),
		}}
	}

	if err := s.client.Create(ctx, env); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil, domain.NewConflict(fmt.Sprintf("sandbox env %q already exists in namespace %s", input.Name, input.Namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	// Materialise the dockerconfigjson Secret after the Env exists so the
	// OwnerRef back to the Env is valid. Failures roll the Env back to
	// avoid leaving an Env whose member Pools can never authenticate to
	// their registry.
	if input.ImagePullSecret != nil && len(input.ImagePullSecret.Registries) > 0 {
		if appErr := s.upsertEnvImagePullSecret(ctx, env, input.ImagePullSecret); appErr != nil {
			if delErr := s.client.Delete(ctx, env); delErr != nil && !k8serrors.IsNotFound(delErr) {
				// Best-effort rollback; surface the original error.
				_ = delErr
			}
			return nil, appErr
		}
	}
	result := envToGen(env)
	return &result, nil
}

// Update merges the editable subset of spec fields into the existing Env,
// retrying on conflict. Pointer fields disambiguate "not specified" from
// "explicit zero/empty"; pass a non-nil pointer to replace, nil to keep.
//
// Validation: when Autoscaling is set, each group's Name must be non-empty
// and Mode (when supplied) must be a known scale-up mode. The CRD's OpenAPI
// validation enforces the remainder once the Patch reaches the apiserver.
func (s *k8sSandboxEnvService) Update(ctx context.Context, input UpdateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError) {
	if appErr := validateEnvAutoscaling(input.Autoscaling); appErr != nil {
		return nil, appErr
	}
	key := types.NamespacedName{Namespace: input.Namespace, Name: input.Name}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		applyEnvUpdate(&current.Spec, input)
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", input.Name, input.Namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	updated := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, key, updated); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	if input.ImagePullSecret != nil && len(input.ImagePullSecret.Registries) > 0 {
		if appErr := s.upsertEnvImagePullSecret(ctx, updated, input.ImagePullSecret); appErr != nil {
			return nil, appErr
		}
	}
	result := envToGen(updated)
	return &result, nil
}

// Delete issues a foreground delete on the Env. Member Pools are
// cascade-deleted via OwnerReferences once the Env Reconciler stamps the
// controller=true OwnerRef.
func (s *k8sSandboxEnvService) Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxEnvResult, *domain.AppError) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", name, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	policy := metav1.DeletePropagationForeground
	if err := s.client.Delete(ctx, env, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", name, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return &gen.DeleteSandboxEnvResult{
		Name:      env.Name,
		Namespace: env.Namespace,
		Status:    "Terminating",
	}, nil
}

// SyncTemplate re-renders every member Pool of the Env against the current
// linked SandboxTemplate body and the Env's overrides. Each member's
// template-name / -version annotations are advanced and its
// EmbeddedSandboxTemplate is patched in place.
//
// Errors from individual members are aggregated — a partial failure leaves
// successful members synced and reports the first failure to the caller so
// it can be retried.
func (s *k8sSandboxEnvService) SyncTemplate(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", name, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	templateName := env.Spec.TemplateRef.Name
	if templateName == "" {
		return nil, domain.NewBadRequest("env.spec.templateRef.name is empty")
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("source template %q not found", templateName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	pools := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, pools, client.InNamespace(namespace)); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Index member-by-name so each pool can be re-rendered with the
	// Env-level overrides + that pool's per-Member resource sizing.
	memberByName := map[string]*agentsv1alpha1.EnvClusterMember{}
	for _, c := range env.Spec.Clusters {
		for i := range c.Members {
			m := &c.Members[i]
			memberByName[m.Name] = m
		}
	}

	var firstErr *domain.AppError
	for i := range pools.Items {
		p := &pools.Items[i]
		if !poolOwnedByEnv(p, env) {
			continue
		}
		opts := composeServiceRenderOptions(env.Spec.Overrides, memberByName[p.Name])
		if err := s.syncMemberPoolToTemplate(ctx, p, tmpl, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}

	updated := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, updated); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	result := envToGen(updated)
	return &result, nil
}

// composeServiceRenderOptions composes the sandboxrender.Apply input from
// Env-level overrides (image) + Member-level resource sizing
// (InlineResources). Mirror of the Env Reconciler's composeRenderOptions
// for the SyncTemplate path.
func composeServiceRenderOptions(envOv *agentsv1alpha1.EnvOverridesSpec, member *agentsv1alpha1.EnvClusterMember) sandboxrender.Options {
	var opts sandboxrender.Options
	if envOv != nil {
		opts.Image = envOv.Image
	}
	if member != nil && member.InlineResources != nil {
		opts.InlineResources = member.InlineResources
	}
	return opts
}

// syncMemberPoolToTemplate re-renders a single member Pool against the
// supplied Template snapshot + composed override options, patches the
// Pool's EmbeddedSandboxTemplate, and advances the template-version
// annotation. Retries on conflict.
func (s *k8sSandboxEnvService) syncMemberPoolToTemplate(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	tmpl *agentsv1alpha1.SandboxTemplate,
	opts sandboxrender.Options,
) *domain.AppError {
	emb := tmpl.Spec.EmbeddedSandboxTemplate
	if err := sandboxrender.Apply(&emb, opts); err != nil {
		return domain.NewBadRequest(err.Error())
	}
	// Re-stamp the Env-level imagePullSecret reference (or remove it if
	// the Secret no longer exists). Matches the Reconciler render path so
	// `POST /envs/{n}/sync-template` and steady-state reconciles agree.
	envName := envNameFromOwnerRefs(pool.OwnerReferences)
	if envName != "" {
		if err := stampPoolImagePullSecretIfPresent(ctx, s.client, pool.Namespace, agentsv1alpha1.EnvImagePullSecretName(envName), &emb); err != nil {
			return domain.NewInternal(err.Error(), err)
		}
	}
	key := types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		current.Spec.EmbeddedSandboxTemplate = emb
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey] = tmpl.Name
		current.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = tmpl.Spec.Version
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	})
	if retryErr != nil {
		if k8serrors.IsNotFound(retryErr) {
			return nil // Pool was deleted concurrently — treat as success.
		}
		return domain.NewInternal(retryErr.Error(), retryErr)
	}
	return nil
}

// poolOwnedByEnv returns true when pool.OwnerReferences includes a
// reference back to env (matched by Kind, Name, and UID).
func poolOwnedByEnv(pool *agentsv1alpha1.SandboxPool, env *agentsv1alpha1.SandboxEnv) bool {
	for _, ref := range pool.OwnerReferences {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind && ref.Name == env.Name && ref.UID == env.UID {
			return true
		}
	}
	return false
}

// applyEnvUpdate merges the non-nil fields of input onto spec in place.
func applyEnvUpdate(spec *agentsv1alpha1.SandboxEnvSpec, input UpdateSandboxEnvInput) {
	if input.Autoscaling != nil {
		spec.Autoscaling = autoscalingFromGen(input.Autoscaling)
	}
	if input.Members != nil {
		setLocalClusterMembers(spec, input.LocalClusterID, *input.Members)
	}
	if input.Overrides != nil {
		spec.Overrides = input.Overrides
	}
}

// setLocalClusterMembers replaces the Members slice on the cluster segment
// matching localClusterID, creating the segment when absent. Passing an
// empty members slice clears the local segment's members (the Reconciler
// then falls back to a single namesake Pool).
func setLocalClusterMembers(spec *agentsv1alpha1.SandboxEnvSpec, localClusterID string, members []agentsv1alpha1.EnvClusterMember) {
	if spec == nil || localClusterID == "" {
		return
	}
	copyMembers := append([]agentsv1alpha1.EnvClusterMember(nil), members...)
	for i := range spec.Clusters {
		if spec.Clusters[i].ClusterID == localClusterID {
			spec.Clusters[i].Members = copyMembers
			return
		}
	}
	spec.Clusters = append(spec.Clusters, agentsv1alpha1.EnvClusterSpec{
		ClusterID: localClusterID,
		Members:   copyMembers,
	})
}

// validateEnvAutoscaling rejects obviously malformed input. Detailed validation
// (e.g. cross-field constraints) belongs in the Env Controller — this layer
// just guards against schema violations the OpenAPI server didn't catch.
func validateEnvAutoscaling(spec *gen.EnvAutoscalingSpec) *domain.AppError {
	if spec == nil {
		return nil
	}
	if spec.Groups == nil {
		return nil
	}
	for i, g := range *spec.Groups {
		if g.Name == "" {
			return domain.NewBadRequest(fmt.Sprintf("autoscaling.groups[%d].name is required", i))
		}
		if g.ScaleUpPolicy != nil && g.ScaleUpPolicy.Mode != nil {
			mode := agentsv1alpha1.PoolScaleUpMode(*g.ScaleUpPolicy.Mode)
			switch mode {
			case agentsv1alpha1.PoolScaleUpModeConservative,
				agentsv1alpha1.PoolScaleUpModeDefault,
				agentsv1alpha1.PoolScaleUpModeAggressive:
			default:
				return domain.NewBadRequest(fmt.Sprintf("autoscaling.groups[%d].scaleUpPolicy.mode %q is invalid", i, *g.ScaleUpPolicy.Mode))
			}
		}
	}
	return nil
}

// envToGen projects a CRD SandboxEnv into the wire shape consumed by the
// dashboard. Mirrors poolToGen in sandboxpool_service.go.
func envToGen(env *agentsv1alpha1.SandboxEnv) gen.SandboxEnv {
	createdAt := env.CreationTimestamp.UTC()
	result := gen.SandboxEnv{
		Name:      env.Name,
		Namespace: env.Namespace,
		Spec:      envSpecToGen(&env.Spec),
		Status:    envStatusToGen(&env.Status),
		Team:      ptr.To(env.Labels[agentsv1alpha1.LabelTeam]),
		User:      ptr.To(env.Labels[agentsv1alpha1.LabelUser]),
		CreatedAt: &createdAt,
	}
	if len(env.Labels) > 0 {
		labels := make(map[string]string, len(env.Labels))
		maps.Copy(labels, env.Labels)
		result.Labels = &labels
	}
	return result
}

func envSpecToGen(spec *agentsv1alpha1.SandboxEnvSpec) gen.SandboxEnvSpec {
	out := gen.SandboxEnvSpec{
		TemplateRef: gen.SandboxEnvTemplateRef{Name: spec.TemplateRef.Name},
		Mode:        gen.SandboxEnvSpecMode(spec.Mode),
	}
	if spec.TemplateRef.Version != "" {
		out.TemplateRef.Version = ptr.To(spec.TemplateRef.Version)
	}
	if spec.Defaults != nil {
		d := gen.SandboxEnvDefaults{}
		if spec.Defaults.InstanceType != "" {
			d.InstanceType = ptr.To(spec.Defaults.InstanceType)
		}
		if spec.Defaults.Multiplier > 0 {
			d.Multiplier = ptr.To(spec.Defaults.Multiplier)
		}
		out.Defaults = &d
	}
	if len(spec.Clusters) > 0 {
		clusters := make([]gen.EnvClusterSpec, 0, len(spec.Clusters))
		for _, c := range spec.Clusters {
			cluster := gen.EnvClusterSpec{ClusterID: c.ClusterID}
			if len(c.Members) > 0 {
				members := make([]gen.EnvClusterMember, 0, len(c.Members))
				for _, m := range c.Members {
					members = append(members, envMemberToGen(m))
				}
				cluster.Members = &members
			}
			clusters = append(clusters, cluster)
		}
		out.Clusters = &clusters
	}
	if spec.Autoscaling != nil {
		out.Autoscaling = autoscalingToGen(spec.Autoscaling)
	}
	if o := envOverridesToGen(spec.Overrides); o != nil {
		out.Overrides = o
	}
	return out
}

func envOverridesToGen(o *agentsv1alpha1.EnvOverridesSpec) *gen.EnvOverrides {
	if o == nil {
		return nil
	}
	out := &gen.EnvOverrides{}
	if o.Image != "" {
		out.Image = ptr.To(o.Image)
	}
	if o.PodCreationImagePolicy != "" {
		p := gen.EnvOverridesPodCreationImagePolicy(o.PodCreationImagePolicy)
		out.PodCreationImagePolicy = &p
	}
	if o.DefaultStartupTimeout != nil {
		out.DefaultStartupTimeout = ptr.To(o.DefaultStartupTimeout.Duration.String())
	}
	if o.DefaultIdleTimeout != nil {
		out.DefaultIdleTimeout = ptr.To(o.DefaultIdleTimeout.Duration.String())
	}
	return out
}

// inlineResourcesToGen flattens corev1.ResourceRequirements into the wire
// ResourceRequirements shape (Quantity string maps). Returns nil when the
// input carries no observable values.
func inlineResourcesToGen(rr *corev1.ResourceRequirements) *gen.ResourceRequirements {
	if rr == nil {
		return nil
	}
	out := &gen.ResourceRequirements{}
	if len(rr.Requests) > 0 {
		req := quantityMapToGen(rr.Requests)
		out.Requests = &req
	}
	if len(rr.Limits) > 0 {
		lim := quantityMapToGen(rr.Limits)
		out.Limits = &lim
	}
	if out.Requests == nil && out.Limits == nil {
		return nil
	}
	return out
}

func quantityMapToGen(rl corev1.ResourceList) map[string]string {
	out := make(map[string]string, len(rl))
	for k, v := range rl {
		out[string(k)] = v.String()
	}
	return out
}

func envMemberToGen(m agentsv1alpha1.EnvClusterMember) gen.EnvClusterMember {
	out := gen.EnvClusterMember{Name: m.Name}
	if m.InstanceType != "" {
		out.InstanceType = ptr.To(m.InstanceType)
	}
	if m.Multiplier > 0 {
		out.Multiplier = ptr.To(m.Multiplier)
	}
	if m.ScalingGroup != "" {
		out.ScalingGroup = ptr.To(m.ScalingGroup)
	}
	if m.MaxReplicas != nil {
		out.MaxReplicas = ptr.To(*m.MaxReplicas)
	}
	if m.Priority != 0 {
		out.Priority = ptr.To(m.Priority)
	}
	if m.ScaleUpPriority != 0 {
		out.ScaleUpPriority = ptr.To(m.ScaleUpPriority)
	}
	if m.ScaleDownPriority != 0 {
		out.ScaleDownPriority = ptr.To(m.ScaleDownPriority)
	}
	if m.Replicas > 0 {
		out.Replicas = ptr.To(m.Replicas)
	}
	if m.InlineResources != nil {
		out.InlineResources = inlineResourcesToGen(m.InlineResources)
	}
	if len(m.Labels) > 0 {
		labels := make(map[string]string, len(m.Labels))
		maps.Copy(labels, m.Labels)
		out.Labels = &labels
	}
	if len(m.Annotations) > 0 {
		annotations := make(map[string]string, len(m.Annotations))
		maps.Copy(annotations, m.Annotations)
		out.Annotations = &annotations
	}
	return out
}

func autoscalingToGen(a *agentsv1alpha1.EnvAutoscalingSpec) *gen.EnvAutoscalingSpec {
	if a == nil {
		return nil
	}
	out := &gen.EnvAutoscalingSpec{}
	enabled := a.Enabled
	out.Enabled = &enabled
	if len(a.Groups) > 0 {
		groups := make([]gen.EnvAutoscalingGroup, 0, len(a.Groups))
		for _, g := range a.Groups {
			groups = append(groups, envGroupToGen(g))
		}
		out.Groups = &groups
	}
	return out
}

func envGroupToGen(g agentsv1alpha1.EnvAutoscalingGroup) gen.EnvAutoscalingGroup {
	out := gen.EnvAutoscalingGroup{Name: g.Name}
	if g.MinReplicas != nil {
		out.MinReplicas = ptr.To(*g.MinReplicas)
	}
	if g.MaxReplicas != nil {
		out.MaxReplicas = ptr.To(*g.MaxReplicas)
	}
	if g.ScaleUpPolicy != nil {
		out.ScaleUpPolicy = scaleUpPolicyToGen(g.ScaleUpPolicy)
	}
	if g.ScaleDownPolicy != nil {
		out.ScaleDownPolicy = scaleDownPolicyToGen(g.ScaleDownPolicy)
	}
	return out
}

func scaleUpPolicyToGen(p *agentsv1alpha1.PoolScaleUpPolicy) *gen.PoolScaleUpPolicy {
	if p == nil {
		return nil
	}
	out := &gen.PoolScaleUpPolicy{}
	if p.Mode != "" {
		mode := gen.PoolScaleUpPolicyMode(p.Mode)
		out.Mode = &mode
	}
	if p.CooldownSeconds > 0 {
		out.CooldownSeconds = ptr.To(p.CooldownSeconds)
	}
	if p.IdleThresholdSeconds > 0 {
		out.IdleThresholdSeconds = ptr.To(p.IdleThresholdSeconds)
	}
	if p.SaturationCooldownSeconds > 0 {
		out.SaturationCooldownSeconds = ptr.To(p.SaturationCooldownSeconds)
	}
	return out
}

func scaleDownPolicyToGen(p *agentsv1alpha1.PoolScaleDownPolicy) *gen.PoolScaleDownPolicy {
	if p == nil {
		return nil
	}
	out := &gen.PoolScaleDownPolicy{}
	if p.IdleTimeoutSeconds > 0 {
		out.IdleTimeoutSeconds = ptr.To(p.IdleTimeoutSeconds)
	}
	if p.StabilizationSeconds > 0 {
		out.StabilizationSeconds = ptr.To(p.StabilizationSeconds)
	}
	if p.ProtectionWindowSeconds > 0 {
		out.ProtectionWindowSeconds = ptr.To(p.ProtectionWindowSeconds)
	}
	return out
}

func envStatusToGen(status *agentsv1alpha1.SandboxEnvStatus) *gen.SandboxEnvStatus {
	if status == nil {
		return nil
	}
	out := &gen.SandboxEnvStatus{}
	if status.PendingRequests > 0 {
		out.PendingRequests = ptr.To(status.PendingRequests)
	}
	if status.LocalMemberCount > 0 {
		out.LocalMemberCount = ptr.To(status.LocalMemberCount)
	}
	if len(status.Conditions) > 0 {
		conds := make([]gen.EnvCondition, 0, len(status.Conditions))
		for _, c := range status.Conditions {
			ec := gen.EnvCondition{Type: c.Type, Status: string(c.Status)}
			if c.Reason != "" {
				ec.Reason = ptr.To(c.Reason)
			}
			if c.Message != "" {
				ec.Message = ptr.To(c.Message)
			}
			if !c.LastTransitionTime.IsZero() {
				t := c.LastTransitionTime.UTC()
				ec.LastTransitionTime = &t
			}
			conds = append(conds, ec)
		}
		out.Conditions = &conds
	}
	if len(status.Clusters) > 0 {
		clusters := make([]gen.EnvClusterStatus, 0, len(status.Clusters))
		for _, c := range status.Clusters {
			clusters = append(clusters, envClusterStatusToGen(c))
		}
		out.Clusters = &clusters
	}
	if len(status.ScalingGroups) > 0 {
		groups := make([]gen.EnvScalingGroupStatus, 0, len(status.ScalingGroups))
		for _, g := range status.ScalingGroups {
			eg := gen.EnvScalingGroupStatus{Name: g.Name}
			if g.TotalIdle > 0 {
				eg.TotalIdle = ptr.To(g.TotalIdle)
			}
			if g.TotalRunning > 0 {
				eg.TotalRunning = ptr.To(g.TotalRunning)
			}
			if g.TotalDesired > 0 {
				eg.TotalDesired = ptr.To(g.TotalDesired)
			}
			if g.TotalPending > 0 {
				eg.TotalPending = ptr.To(g.TotalPending)
			}
			groups = append(groups, eg)
		}
		out.ScalingGroups = &groups
	}
	return out
}

func envClusterStatusToGen(c agentsv1alpha1.EnvClusterStatus) gen.EnvClusterStatus {
	out := gen.EnvClusterStatus{ClusterID: c.ClusterID}
	if c.IsLocal {
		out.IsLocal = ptr.To(true)
	}
	if c.LastScaleUpTime != nil {
		t := c.LastScaleUpTime.UTC()
		out.LastScaleUpTime = &t
	}
	if c.LastScaleDownTime != nil {
		t := c.LastScaleDownTime.UTC()
		out.LastScaleDownTime = &t
	}
	if c.IdleZeroSince != nil {
		t := c.IdleZeroSince.UTC()
		out.IdleZeroSince = &t
	}
	if c.LastSnapshotTime != nil {
		t := c.LastSnapshotTime.UTC()
		out.LastSnapshotTime = &t
	}
	if len(c.ObservedMembers) > 0 {
		members := make([]gen.EnvObservedMember, 0, len(c.ObservedMembers))
		for _, m := range c.ObservedMembers {
			members = append(members, envObservedMemberToGen(m))
		}
		out.ObservedMembers = &members
	}
	return out
}

func envObservedMemberToGen(m agentsv1alpha1.EnvObservedMember) gen.EnvObservedMember {
	out := gen.EnvObservedMember{Name: m.Name}
	if m.InstanceType != "" {
		out.InstanceType = ptr.To(m.InstanceType)
	}
	if m.Multiplier > 0 {
		out.Multiplier = ptr.To(m.Multiplier)
	}
	if m.State != "" {
		state := gen.EnvObservedMemberState(m.State)
		out.State = &state
	}
	if m.IdleCount > 0 {
		out.IdleCount = ptr.To(m.IdleCount)
	}
	if m.RunningCount > 0 {
		out.RunningCount = ptr.To(m.RunningCount)
	}
	if m.DesiredReplicas > 0 {
		out.DesiredReplicas = ptr.To(m.DesiredReplicas)
	}
	if m.CurrentReplicas > 0 {
		out.CurrentReplicas = ptr.To(m.CurrentReplicas)
	}
	if m.PendingRequests > 0 {
		out.PendingRequests = ptr.To(m.PendingRequests)
	}
	if m.SaturatedUntil != nil {
		t := m.SaturatedUntil.UTC()
		out.SaturatedUntil = &t
	}
	if m.LastScaleUpAttemptResult != "" {
		out.LastScaleUpAttemptResult = ptr.To(m.LastScaleUpAttemptResult)
	}
	if m.ScaleUpErrorMessage != "" {
		out.ScaleUpErrorMessage = ptr.To(m.ScaleUpErrorMessage)
	}
	return out
}

// autoscalingFromGen converts the wire shape back into the CRD type so we
// can persist edits coming from the dashboard.
func autoscalingFromGen(a *gen.EnvAutoscalingSpec) *agentsv1alpha1.EnvAutoscalingSpec {
	if a == nil {
		return nil
	}
	out := &agentsv1alpha1.EnvAutoscalingSpec{}
	if a.Enabled != nil {
		out.Enabled = *a.Enabled
	}
	if a.Groups != nil {
		for _, g := range *a.Groups {
			out.Groups = append(out.Groups, groupFromGen(g))
		}
	}
	return out
}

func groupFromGen(g gen.EnvAutoscalingGroup) agentsv1alpha1.EnvAutoscalingGroup {
	out := agentsv1alpha1.EnvAutoscalingGroup{Name: g.Name}
	if g.MinReplicas != nil {
		out.MinReplicas = ptr.To(*g.MinReplicas)
	}
	if g.MaxReplicas != nil {
		out.MaxReplicas = ptr.To(*g.MaxReplicas)
	}
	if g.ScaleUpPolicy != nil {
		out.ScaleUpPolicy = scaleUpPolicyFromGen(g.ScaleUpPolicy)
	}
	if g.ScaleDownPolicy != nil {
		out.ScaleDownPolicy = scaleDownPolicyFromGen(g.ScaleDownPolicy)
	}
	return out
}

func scaleUpPolicyFromGen(p *gen.PoolScaleUpPolicy) *agentsv1alpha1.PoolScaleUpPolicy {
	if p == nil {
		return nil
	}
	out := &agentsv1alpha1.PoolScaleUpPolicy{}
	if p.Mode != nil {
		out.Mode = agentsv1alpha1.PoolScaleUpMode(*p.Mode)
	}
	if p.CooldownSeconds != nil {
		out.CooldownSeconds = *p.CooldownSeconds
	}
	if p.IdleThresholdSeconds != nil {
		out.IdleThresholdSeconds = *p.IdleThresholdSeconds
	}
	if p.SaturationCooldownSeconds != nil {
		out.SaturationCooldownSeconds = *p.SaturationCooldownSeconds
	}
	return out
}

func scaleDownPolicyFromGen(p *gen.PoolScaleDownPolicy) *agentsv1alpha1.PoolScaleDownPolicy {
	if p == nil {
		return nil
	}
	out := &agentsv1alpha1.PoolScaleDownPolicy{}
	if p.IdleTimeoutSeconds != nil {
		out.IdleTimeoutSeconds = *p.IdleTimeoutSeconds
	}
	if p.StabilizationSeconds != nil {
		out.StabilizationSeconds = *p.StabilizationSeconds
	}
	if p.ProtectionWindowSeconds != nil {
		out.ProtectionWindowSeconds = *p.ProtectionWindowSeconds
	}
	return out
}

// =========================================================================
// Env-scoped Pool CRUD
//
// Each "Pool" is an entry of env.spec.clusters[].members[] — the Env
// Reconciler materialises the matching SandboxPool CR. These methods mutate
// the Env spec only; the SandboxPool object on List/Get is read straight
// from the API server (projected via poolToGen).
// =========================================================================

// AddMemberPool appends a new member to env's local cluster segment and
// returns the projected SandboxPool. The server derives member.Name and
// member.ScalingGroup from the supplied resources + quota label; any
// caller-supplied values for these fields are overwritten. Plugin admission
// (PreCreatePool) runs after derivation and before the Env patch; mutations
// the plugin makes to labels/annotations/replicas are propagated back to
// the member so the Env Reconciler materialises the eventual SandboxPool
// CR with them.
func (s *k8sSandboxEnvService) AddMemberPool(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	derived, appErr := derivePoolMember(ctx, s.instProv, s.quotaProv, envName, member)
	if appErr != nil {
		return nil, appErr
	}
	member = derived
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	// Load env once to build the admission candidate. Refetched inside the
	// retry loop for the actual patch — admission is run only once even on
	// conflict-retries because the plugin hooks may be non-idempotent (e.g.
	// quota reservation).
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, key, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	for _, m := range localClusterMembers(&env.Spec, localClusterID) {
		if m.Name == member.Name {
			return nil, domain.NewConflict(fmt.Sprintf("member pool %q already exists in env %q (derived from resources + quota)", member.Name, envName))
		}
	}

	candidate := buildPoolCandidate(env, member)
	preLabels, preAnnotations, preReplicas := snapshotPoolMutables(candidate)
	if appErr := s.admitter.AdmitCreate(ctx, candidate); appErr != nil {
		return nil, appErr
	}
	mergePluginMutations(&member, candidate, preLabels, preAnnotations, preReplicas)

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		members := localClusterMembers(&current.Spec, localClusterID)
		for _, m := range members {
			if m.Name == member.Name {
				return &domain.AppError{Code: domain.ErrCodeConflict,
					Message: fmt.Sprintf("member pool %q already exists in env %q", member.Name, envName)}
			}
		}
		setLocalClusterMembers(&current.Spec, localClusterID, append(members, member))
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			return nil, appErr
		}
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return s.projectMemberPool(ctx, namespace, envName, member), nil
}

// UpdateMemberPool adjusts the named member's replica counts. Resource
// shape (instanceType / inlineResources / multiplier), labels, and
// annotations are immutable post-create — callers must Delete + Add to
// change them. When the member's ScalingGroup has autoscaling enabled, the
// Replicas field is rejected (the autoscaler owns it); MaxReplicas is
// always accepted.
//
// Plugin admission (PreUpdatePool) runs against the overlaid candidate
// before the Env patch lands.
func (s *k8sSandboxEnvService) UpdateMemberPool(ctx context.Context, namespace, envName, poolName, localClusterID string, patch MemberPoolPatch) (*gen.SandboxPool, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	if patch.Replicas == nil && patch.MaxReplicas == nil {
		return nil, domain.NewBadRequest("at least one of replicas or maxReplicas must be provided")
	}
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, key, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	members := localClusterMembers(&env.Spec, localClusterID)
	var existing *agentsv1alpha1.EnvClusterMember
	for i := range members {
		if members[i].Name == poolName {
			existing = &members[i]
			break
		}
	}
	if existing == nil {
		return nil, domain.NewNotFound(fmt.Sprintf("member pool %q not found in env %q", poolName, envName))
	}
	if patch.Replicas != nil && scalingGroupHasAutoscaling(env, existing.ScalingGroup) {
		return nil, domain.NewBadRequest(fmt.Sprintf("replicas is owned by the autoscaler for scalingGroup %q; only maxReplicas can be edited", existing.ScalingGroup))
	}

	// Build the updated member by overlaying the patch onto the existing
	// entry — preserves resources, labels, annotations, name, scalingGroup.
	member := *existing
	if patch.Replicas != nil {
		member.Replicas = *patch.Replicas
	}
	if patch.MaxReplicas != nil {
		v := *patch.MaxReplicas
		member.MaxReplicas = &v
	}

	candidate, err := s.loadOrBuildCandidate(ctx, env, member)
	if err != nil {
		return nil, err
	}
	preLabels, preAnnotations, preReplicas := snapshotPoolMutables(candidate)
	// Pod list (driver-supplied for PreUpdatePool) is left empty here; the
	// Reconciler still owns the in-cluster update path that supplies live
	// pods. This is a coarse pre-check at the API edge.
	if _, appErr := s.admitter.AdmitUpdate(ctx, candidate, nil); appErr != nil {
		return nil, appErr
	}
	mergePluginMutations(&member, candidate, preLabels, preAnnotations, preReplicas)

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		ms := localClusterMembers(&current.Spec, localClusterID)
		idx := -1
		for i, m := range ms {
			if m.Name == poolName {
				idx = i
				break
			}
		}
		if idx < 0 {
			return &domain.AppError{Code: domain.ErrCodeNotFound,
				Message: fmt.Sprintf("member pool %q not found in env %q", poolName, envName)}
		}
		ms[idx] = member
		setLocalClusterMembers(&current.Spec, localClusterID, ms)
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			return nil, appErr
		}
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return s.projectMemberPool(ctx, namespace, envName, member), nil
}

// DeleteMemberPool removes the named member from env's local cluster
// segment. The Reconciler cascade-deletes the SandboxPool CR. Plugin
// admission (PreDeletePool) runs against the live Pool CR — when the Pool
// has not been materialised yet, admission is skipped (nothing for the
// plugin to release).
func (s *k8sSandboxEnvService) DeleteMemberPool(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	// Run delete admission against the live Pool when present. Missing Pool
	// (Reconciler hasn't created it yet) means there's no plugin state to
	// release, so we skip admission and proceed to the Env patch.
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err == nil {
		if envNameFromOwnerRefs(pool.OwnerReferences) != envName {
			return nil, domain.NewNotFound(fmt.Sprintf("member pool %q not found in env %q", poolName, envName))
		}
		if appErr := s.admitter.AdmitDelete(ctx, pool); appErr != nil {
			return nil, appErr
		}
	} else if !k8serrors.IsNotFound(err) {
		return nil, domain.NewInternal(err.Error(), err)
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		members := localClusterMembers(&current.Spec, localClusterID)
		out := members[:0]
		found := false
		for _, m := range members {
			if m.Name == poolName {
				found = true
				continue
			}
			out = append(out, m)
		}
		if !found {
			return &domain.AppError{Code: domain.ErrCodeNotFound,
				Message: fmt.Sprintf("member pool %q not found in env %q", poolName, envName)}
		}
		setLocalClusterMembers(&current.Spec, localClusterID, append([]agentsv1alpha1.EnvClusterMember(nil), out...))
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			return nil, appErr
		}
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return &gen.DeleteSandboxPoolResult{
		Name:      poolName,
		Namespace: namespace,
		Status:    "Terminating",
	}, nil
}

// ListMemberPools enumerates SandboxPool CRs in namespace whose OwnerRefs
// point at envName. Projects each via poolToGen for the wire shape.
func (s *k8sSandboxEnvService) ListMemberPools(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError) {
	if _, appErr := s.Get(ctx, namespace, envName); appErr != nil {
		return nil, appErr
	}
	pools := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, pools, client.InNamespace(namespace)); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	items := make([]gen.SandboxPool, 0)
	for i := range pools.Items {
		p := &pools.Items[i]
		if envNameFromOwnerRefs(p.OwnerReferences) != envName {
			continue
		}
		items = append(items, poolToGen(ctx, p, nil))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

// GetMemberPool fetches one SandboxPool CR and verifies its ownership.
func (s *k8sSandboxEnvService) GetMemberPool(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if envNameFromOwnerRefs(pool.OwnerReferences) != envName {
		return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
	}
	result := poolToGen(ctx, pool, nil)
	return &result, nil
}

// projectMemberPool returns the freshly materialised SandboxPool CR if it
// exists, else a minimal projection from the member fields. Used by
// Add/Update which return immediately after the Env Patch lands but before
// the Reconciler has had a chance to run.
func (s *k8sSandboxEnvService) projectMemberPool(ctx context.Context, namespace, envName string, member agentsv1alpha1.EnvClusterMember) *gen.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: member.Name}, pool); err == nil &&
		envNameFromOwnerRefs(pool.OwnerReferences) == envName {
		result := poolToGen(ctx, pool, nil)
		return &result
	}
	return &gen.SandboxPool{
		Name:      member.Name,
		Namespace: namespace,
		Spec:      gen.SandboxPoolSpec{Replicas: member.Replicas},
		OwningEnv: ptr.To(envName),
	}
}

// localClusterMembers returns a copy of the member list for the cluster
// segment matching localClusterID. An empty result includes both "segment
// absent" and "segment present with empty members".
func localClusterMembers(spec *agentsv1alpha1.SandboxEnvSpec, localClusterID string) []agentsv1alpha1.EnvClusterMember {
	if spec == nil {
		return nil
	}
	for _, c := range spec.Clusters {
		if c.ClusterID == localClusterID {
			return append([]agentsv1alpha1.EnvClusterMember(nil), c.Members...)
		}
	}
	return nil
}

// buildPoolCandidate synthesises the prospective Pool object passed to
// PreCreatePool plugin hooks. Mirrors the subset of the Env Reconciler's
// pool synthesis that plugins care about (identity labels, member labels
// and annotations, replicas, template ref). The rendered pod spec is left
// empty — quota / scheduling plugins inspect labels and replicas, not the
// pod template.
func buildPoolCandidate(env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember) *agentsv1alpha1.SandboxPool {
	labels := map[string]string{}
	if v := env.Labels[agentsv1alpha1.LabelTeam]; v != "" {
		labels[agentsv1alpha1.LabelTeam] = v
	}
	if v := env.Labels[agentsv1alpha1.LabelUser]; v != "" {
		labels[agentsv1alpha1.LabelUser] = v
	}
	maps.Copy(labels, member.Labels)
	var annotations map[string]string
	if len(member.Annotations) > 0 {
		annotations = make(map[string]string, len(member.Annotations))
		maps.Copy(annotations, member.Annotations)
	}
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        member.Name,
			Namespace:   env.Namespace,
			Labels:      labels,
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       env.Name,
				UID:        env.UID,
			}},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:     member.Replicas,
			TemplateName: env.Spec.TemplateRef.Name,
		},
	}
	if env.Spec.Overrides != nil {
		pool.Spec.PodCreationImagePolicy = env.Spec.Overrides.PodCreationImagePolicy
		pool.Spec.DefaultStartupTimeout = env.Spec.Overrides.DefaultStartupTimeout
		pool.Spec.DefaultIdleTimeout = env.Spec.Overrides.DefaultIdleTimeout
	}
	return pool
}

// snapshotPoolMutables captures the pre-admission state of the fields a
// plugin may mutate so mergePluginMutations can detect changes.
func snapshotPoolMutables(p *agentsv1alpha1.SandboxPool) (labels, annotations map[string]string, replicas int32) {
	labels = make(map[string]string, len(p.Labels))
	maps.Copy(labels, p.Labels)
	annotations = make(map[string]string, len(p.Annotations))
	maps.Copy(annotations, p.Annotations)
	replicas = p.Spec.Replicas
	return
}

// mergePluginMutations propagates plugin-added labels/annotations and any
// replica adjustments from the candidate Pool back to the EnvClusterMember.
// Only newly added keys (absent or different from pre-admission snapshot)
// are copied so we don't leak env-identity labels into the member shape.
func mergePluginMutations(member *agentsv1alpha1.EnvClusterMember, candidate *agentsv1alpha1.SandboxPool, preLabels, preAnnotations map[string]string, preReplicas int32) {
	for k, v := range candidate.Labels {
		if preLabels[k] == v {
			continue
		}
		if member.Labels == nil {
			member.Labels = map[string]string{}
		}
		member.Labels[k] = v
	}
	for k, v := range candidate.Annotations {
		if preAnnotations[k] == v {
			continue
		}
		if member.Annotations == nil {
			member.Annotations = map[string]string{}
		}
		member.Annotations[k] = v
	}
	if candidate.Spec.Replicas != preReplicas {
		member.Replicas = candidate.Spec.Replicas
	}
}

// loadOrBuildCandidate returns the live SandboxPool with the updated
// member's fields overlaid, or a freshly built candidate when the Pool has
// not been materialised yet. Used as input to PreUpdatePool admission.
func (s *k8sSandboxEnvService) loadOrBuildCandidate(ctx context.Context, env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember) (*agentsv1alpha1.SandboxPool, *domain.AppError) {
	live := &agentsv1alpha1.SandboxPool{}
	err := s.client.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: member.Name}, live)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return buildPoolCandidate(env, member), nil
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	overlay := live.DeepCopy()
	if member.Replicas != 0 {
		overlay.Spec.Replicas = member.Replicas
	}
	for k, v := range member.Labels {
		if overlay.Labels == nil {
			overlay.Labels = map[string]string{}
		}
		overlay.Labels[k] = v
	}
	for k, v := range member.Annotations {
		if overlay.Annotations == nil {
			overlay.Annotations = map[string]string{}
		}
		overlay.Annotations[k] = v
	}
	return overlay, nil
}
