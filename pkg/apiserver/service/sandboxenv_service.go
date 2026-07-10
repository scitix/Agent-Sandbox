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
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envautoscaler"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// EnvShellService is the env-only CRUD slice of SandboxEnvService. Lives as
// its own interface so the composed SandboxEnvService can be assembled
// from three orthogonal contracts (env shell + member CRUD + autoscaler
// CRUD) without method-name collisions.
type EnvShellService interface {
	// List returns a lightweight summary of the SandboxEnvs in namespace
	// visible to the caller. When team/user are non-empty the result is
	// filtered by the standard scheduling.navix.sh/{team,user} labels
	// (matching the Pool model). Callers needing the full spec/status fetch
	// each Env via Get.
	List(ctx context.Context, namespace, team, user string) ([]gen.SandboxEnvSummary, *domain.AppError)
	// Get returns a single Env or NotFound.
	Get(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError)
	// Create posts a new SandboxEnv shell. The body carries TemplateRef +
	// Overrides + optional ImagePullSecret only — Members are added via
	// AddMember (from the embedded MemberPoolService), which also
	// materialises the matching autoscaling group automatically.
	Create(ctx context.Context, input CreateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError)
	// Update patches Overrides / ImagePullSecret on an existing Env.
	// Members and autoscaling groups are intentionally not part of this
	// surface — use the dedicated CRUD methods from the embedded
	// sub-services to avoid accidental wholesale replacement.
	Update(ctx context.Context, input UpdateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError)
	// Delete issues a foreground delete on the Env. Member Pools are
	// cascade-deleted via OwnerReferences (controller=true,
	// blockOwnerDeletion=true) stamped by the Env Reconciler.
	Delete(ctx context.Context, namespace, name string) (*gen.DeleteSandboxEnvResult, *domain.AppError)
	// SyncTemplate re-renders every member SandboxPool against the current
	// SandboxTemplate body + the Env's overrides, advancing each Pool's
	// template-version annotation. Use this after an admin edits the
	// underlying Template — Env-level overrides edits propagate
	// automatically via Update().
	SyncTemplate(ctx context.Context, namespace, name string) (*gen.SandboxEnv, *domain.AppError)
	// ListEvents returns recent K8s Events emitted against the Env and its
	// member SandboxPools, merged and sorted descending by lastTimestamp.
	// Drives the activity timeline on the Env detail page in the dashboard.
	ListEvents(ctx context.Context, namespace, name string, limit int) ([]gen.EnvEvent, *domain.AppError)
}

// SandboxEnvService is the business-layer surface for SandboxEnv resources.
// It composes the env shell CRUD with the two sub-resource interfaces
// (MemberPool CRUD + Autoscaler CRUD) so the router only needs to inject
// one service instance and handlers call e.g. s.env.AddMember(...) or
// s.env.UpdateAutoscalingGroup(...).
//
// Method names on the sub-interfaces carry a noun suffix (AddMember,
// UpdateAutoscalingGroup, …) so embedding doesn't produce conflicts and
// the call site reads naturally.
type SandboxEnvService interface {
	EnvShellService
	envmember.MemberPoolService
	envautoscaler.AutoscalerService
}

// CreateSandboxEnvInput is the parsed CreateSandboxEnvRequest with auth
// context resolved. Members and Autoscaling are intentionally absent — the
// caller adds them via the embedded sub-services after the env shell is
// created.
type CreateSandboxEnvInput struct {
	Name      string
	Namespace string
	Team      string // copied from auth, injected as label
	User      string // copied from auth, injected as label

	TemplateRef agentsv1alpha1.SandboxEnvTemplateRef
	Mode        agentsv1alpha1.SandboxEnvMode
	Overrides   *agentsv1alpha1.EnvOverridesSpec
	// ImagePullSecret, when non-nil, instructs the service to materialise a
	// dockerconfigjson Secret named ips-{envName} with an OwnerRef pointing
	// at the Env (cascade-delete free). The Env Reconciler stamps a
	// LocalObjectReference to that Secret into every member Pool.
	ImagePullSecret *gen.ImagePullSecretInput

	Labels      map[string]string
	Annotations map[string]string
}

// UpdateSandboxEnvInput carries the editable patch for an Env shell.
// Members and Autoscaling are intentionally not exposed here so callers
// can't accidentally wholesale-replace either set; use the dedicated
// sub-service methods instead.
type UpdateSandboxEnvInput struct {
	Name      string
	Namespace string

	Overrides *agentsv1alpha1.EnvOverridesSpec
	// ImagePullSecret, when non-nil, upserts the dockerconfigjson Secret
	// backing this Env's image-pull credentials. Nil means leave existing
	// Secret untouched.
	ImagePullSecret *gen.ImagePullSecretInput
}

// k8sSandboxEnvService is the default SandboxEnvService implementation. The
// env-shell methods live on this receiver directly; member-pool and
// autoscaler methods come from the embedded sub-services.
type k8sSandboxEnvService struct {
	client client.Client

	// Embedded sub-services — their methods promote to satisfy the
	// composed SandboxEnvService interface.
	envmember.MemberPoolService
	envautoscaler.AutoscalerService
}

// NewSandboxEnvService constructs the default service implementation. The
// PluginManager / provider arguments flow through to the embedded
// MemberPoolService; the autoscaler sub-service currently needs only the
// k8s client. A nil *plugins.PluginManager is treated as "no plugins";
// nil providers are normalised to their Noop forms inside envmember.New.
func NewSandboxEnvService(c client.Client, pm *plugins.PluginManager, instProv instancetypeplugin.Provider, quotaProv quotaplugin.Provider) SandboxEnvService {
	return &k8sSandboxEnvService{
		client:            c,
		MemberPoolService: envmember.New(c, pm, instProv, quotaProv),
		AutoscalerService: envautoscaler.New(c),
	}
}

// List enumerates Envs in the namespace, filtered by team/user labels when
// supplied. Sorted by name for deterministic test output.
func (s *k8sSandboxEnvService) List(ctx context.Context, namespace, team, user string) ([]gen.SandboxEnvSummary, *domain.AppError) {
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
	items := make([]gen.SandboxEnvSummary, 0, len(envList.Items))
	for i := range envList.Items {
		items = append(items, envToSummary(&envList.Items[i]))
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
	s.enrichEnvDocs(ctx, env, &result)
	return &result, nil
}

// enrichEnvDocs populates result.EnvDocs from the linked SandboxTemplate's
// agentbox.navix.sh/docs annotation. Best-effort: lookup failures leave the
// field nil so the handler can return the env without docs rather than
// failing the whole request. The handler does the placeholder substitution
// (env name / cluster id / api key) on top of the raw value.
func (s *k8sSandboxEnvService) enrichEnvDocs(ctx context.Context, env *agentsv1alpha1.SandboxEnv, out *gen.SandboxEnv) {
	if env.Spec.TemplateRef.Name == "" {
		return
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: env.Spec.TemplateRef.Name}, tmpl); err != nil {
		return
	}
	if v := tmpl.Annotations[agentsv1alpha1.SandboxTemplateDocsAnnotationKey]; v != "" {
		out.EnvDocs = ptr.To(v)
	}
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

// Update patches Overrides / ImagePullSecret on an existing Env. Members
// and autoscaling groups are managed through the dedicated sub-service
// methods (AddMember / UpdateAutoscalingGroup / …) so accidental wholesale
// replacement isn't possible here.
func (s *k8sSandboxEnvService) Update(ctx context.Context, input UpdateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError) {
	key := types.NamespacedName{Namespace: input.Namespace, Name: input.Name}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		if input.Overrides != nil {
			current.Spec.Overrides = input.Overrides
		}
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

	ipsExists, _ := poolrender.ImagePullSecretExists(ctx, s.client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name))

	var firstErr *domain.AppError
	for i := range pools.Items {
		p := &pools.Items[i]
		if !poolOwnedByEnv(p, env) {
			continue
		}
		// Reuse the same RenderSandboxPool the Reconciler runs so
		// sync-template and steady-state reconciles converge to identical
		// pod specs. Members declared in spec.clusters[].members[] have a
		// matching entry in memberByName; legacy pools that predate that
		// spec fall back to a namesake member shape just like the
		// Reconciler's desiredLocalMembers does.
		member := agentsv1alpha1.EnvClusterMember{Name: p.Name}
		if m, ok := memberByName[p.Name]; ok {
			member = *m
		}
		rendered, err := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env:                   env,
			Template:              tmpl,
			Member:                member,
			ImagePullSecretExists: ipsExists,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = domain.NewBadRequest(err.Error())
			}
			continue
		}
		if appErr := s.syncMemberPoolToRendered(ctx, p, tmpl, rendered); appErr != nil && firstErr == nil {
			firstErr = appErr
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

// syncMemberPoolToRendered patches the live Pool to match the freshly
// rendered want (produced by poolrender.RenderSandboxPool against the
// current Template body + Env overrides) and advances the template-version
// annotation to reflect the Template the caller targeted. Retries on
// conflict. A concurrent delete is treated as success — the Reconciler
// will pick up the divergence on the next pass.
func (s *k8sSandboxEnvService) syncMemberPoolToRendered(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	tmpl *agentsv1alpha1.SandboxTemplate,
	want *agentsv1alpha1.SandboxPool,
) *domain.AppError {
	key := types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		current.Spec.EmbeddedSandboxTemplate = *want.Spec.EmbeddedSandboxTemplate.DeepCopy()
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

// envToGen projects a CRD SandboxEnv into the wire shape consumed by the
// dashboard. Mirrors PoolToGen in sandboxpool_service.go.
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

// envToSummary projects a SandboxEnv onto the lightweight List shape. It
// carries only what the list table and its row links need — identity, the
// bound template name, mode, the env-wide replica rollups, autoscaling group
// counts, and a Ready flag — deliberately omitting the autoscaling policies,
// per-member config, and detailed status that only the detail page (Get)
// requires.
func envToSummary(env *agentsv1alpha1.SandboxEnv) gen.SandboxEnvSummary {
	createdAt := env.CreationTimestamp.UTC()
	out := gen.SandboxEnvSummary{
		Name:         env.Name,
		Namespace:    ptr.To(env.Namespace),
		TemplateName: ptr.To(env.Spec.TemplateRef.Name),
		Mode:         ptr.To(gen.SandboxEnvSummaryMode(env.Spec.Mode)),
		Team:         ptr.To(env.Labels[agentsv1alpha1.LabelTeam]),
		User:         ptr.To(env.Labels[agentsv1alpha1.LabelUser]),
		CreatedAt:    &createdAt,
	}

	st := &env.Status
	if st.MemberCount > 0 {
		out.MemberCount = ptr.To(st.MemberCount)
	}
	if st.DesiredReplicas > 0 {
		out.DesiredReplicas = ptr.To(st.DesiredReplicas)
	}
	if st.RunningReplicas > 0 {
		out.RunningReplicas = ptr.To(st.RunningReplicas)
	}
	if st.IdleReplicas > 0 {
		out.IdleReplicas = ptr.To(st.IdleReplicas)
	}

	// Autoscaling is toggled per group — there is no Env-level switch — so the
	// list surfaces the enabled/total group counts rather than a single bit.
	if env.Spec.Autoscaling != nil && len(env.Spec.Autoscaling.Groups) > 0 {
		groups := env.Spec.Autoscaling.Groups
		enabled := 0
		for i := range groups {
			if groups[i].Enabled {
				enabled++
			}
		}
		out.ScalingGroupCount = ptr.To(int32(len(groups)))
		out.AutoscalingEnabledGroupCount = ptr.To(int32(enabled))
	}

	out.Ready = ptr.To(envReady(env))
	return out
}

// envReady reports whether the Env's Ready condition is currently True.
func envReady(env *agentsv1alpha1.SandboxEnv) bool {
	for i := range env.Status.Conditions {
		c := &env.Status.Conditions[i]
		if c.Type == agentsv1alpha1.SandboxEnvConditionReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
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
		out.Autoscaling = envautoscaler.SpecToGen(spec.Autoscaling)
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
	out.NetworkPolicy = networkPolicyToGen(o.NetworkPolicy)
	return out
}

// networkPolicyToGen maps the CRD egress policy onto the wire shape (GET).
func networkPolicyToGen(np *agentsv1alpha1.SandboxNetworkPolicy) *gen.SandboxNetworkPolicy {
	if np == nil {
		return nil
	}
	g := &gen.SandboxNetworkPolicy{}
	if np.DisableEgress {
		g.DisableEgress = ptr.To(true)
	}
	if np.AllowPrivateNetworks {
		g.AllowPrivateNetworks = ptr.To(true)
	}
	if np.Egress != nil {
		e := &gen.EgressRules{}
		if len(np.Egress.AllowedDomains) > 0 {
			e.AllowedDomains = ptr.To(np.Egress.AllowedDomains)
		}
		if len(np.Egress.AllowedCIDRs) > 0 {
			e.AllowedCIDRs = ptr.To(np.Egress.AllowedCIDRs)
		}
		if len(np.Egress.DeniedCIDRs) > 0 {
			e.DeniedCIDRs = ptr.To(np.Egress.DeniedCIDRs)
		}
		g.Egress = e
	}
	return g
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
	cfg := envMemberConfigToGen(m.Config)
	if cfg != nil {
		out.Config = cfg
	}
	return out
}

// envMemberConfigToGen projects an EnvClusterMemberConfig to the wire
// shape. Returns nil when no field is populated so the JSON output
// stays clean.
func envMemberConfigToGen(c agentsv1alpha1.EnvClusterMemberConfig) *gen.EnvClusterMemberConfig {
	out := &gen.EnvClusterMemberConfig{}
	populated := false
	if c.InstanceType != "" {
		out.InstanceType = ptr.To(c.InstanceType)
		populated = true
	}
	if c.Multiplier > 0 {
		out.Multiplier = ptr.To(c.Multiplier)
		populated = true
	}
	if c.ScalingGroup != "" {
		out.ScalingGroup = ptr.To(c.ScalingGroup)
		populated = true
	}
	if c.MinReplicas != nil {
		out.MinReplicas = ptr.To(*c.MinReplicas)
		populated = true
	}
	if c.MaxReplicas != nil {
		out.MaxReplicas = ptr.To(*c.MaxReplicas)
		populated = true
	}
	if c.Priority != 0 {
		out.Priority = ptr.To(c.Priority)
		populated = true
	}
	if c.ScaleUpPriority != nil {
		out.ScaleUpPriority = ptr.To(*c.ScaleUpPriority)
		populated = true
	}
	if c.ScaleDownPriority != nil {
		out.ScaleDownPriority = ptr.To(*c.ScaleDownPriority)
		populated = true
	}
	if c.InlineResources != nil {
		out.InlineResources = inlineResourcesToGen(c.InlineResources)
		populated = true
	}
	if len(c.Labels) > 0 {
		labels := make(map[string]string, len(c.Labels))
		maps.Copy(labels, c.Labels)
		out.Labels = &labels
		populated = true
	}
	if len(c.Annotations) > 0 {
		annotations := make(map[string]string, len(c.Annotations))
		maps.Copy(annotations, c.Annotations)
		out.Annotations = &annotations
		populated = true
	}
	if !populated {
		return nil
	}
	return out
}

func envStatusToGen(status *agentsv1alpha1.SandboxEnvStatus) *gen.SandboxEnvStatus {
	if status == nil {
		return nil
	}
	out := &gen.SandboxEnvStatus{}
	if status.MemberCount > 0 {
		out.MemberCount = ptr.To(status.MemberCount)
	}
	if status.DesiredReplicas > 0 {
		out.DesiredReplicas = ptr.To(status.DesiredReplicas)
	}
	if status.RunningReplicas > 0 {
		out.RunningReplicas = ptr.To(status.RunningReplicas)
	}
	if status.IdleReplicas > 0 {
		out.IdleReplicas = ptr.To(status.IdleReplicas)
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
	return out
}
