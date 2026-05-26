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

// Package envmember serves the Env-scoped Pool CRUD surface:
// `/v1/sandboxenvs/{name}/sandboxpools/*`. Each "member pool" is an entry
// of env.spec.clusters[].members[]; the SandboxEnv Reconciler materialises
// the matching SandboxPool CR.
//
// This package exists to keep the SandboxEnvService surface focused on the
// Env object itself. The Member CRUD path has its own admission flow
// (PreCreatePool / PreUpdatePool / PreDeletePool plugins), its own naming
// derivation (instanceType + quota), and its own projection helpers — none
// of which the Env spec CRUD cares about. The two services share the
// SandboxEnv K8s client; plugin admission is invoked via a
// *plugins.PluginManager (nil-safe — open-source builds pass nil).
//
// Render contract: AddMemberPool and UpdateMemberPool build the prospective
// SandboxPool via poolrender.RenderSandboxPool — the exact same function
// the Reconciler runs against env.spec.clusters[*].members[*]. Plugin
// admission therefore sees a candidate that's byte-equal to what the
// Reconciler will eventually persist (modulo Reconciler-only side-effects
// such as the dynamic image-pull-secret stamp).
package envmember

import (
	"context"
	"fmt"
	"maps"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envcommon"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// Service is the Env-scoped Pool CRUD surface. Routes onto
// `/v1/sandboxenvs/{name}/sandboxpools/*` via handlers/server.go.
type MemberPoolService interface {
	// Add appends a new member to env's local cluster segment. The server
	// derives member.Name and member.ScalingGroup from the supplied
	// resources + quota label; any caller-supplied values for these fields
	// are overwritten. Plugin admission (PreCreatePool) runs against the
	// fully rendered prospective SandboxPool before the Env patch lands.
	AddMember(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError)
	// Update adjusts the named member's replica counts. Resource shape,
	// instanceType, labels, and annotations are immutable post-create —
	// callers must Delete + Add to change them. When the member's
	// ScalingGroup has autoscaling enabled, Replicas is rejected.
	UpdateMember(ctx context.Context, namespace, envName, poolName, localClusterID string, patch MemberPoolPatch) (*gen.SandboxPool, *domain.AppError)
	// Delete removes the named member from env's local cluster segment.
	// The Reconciler cascade-deletes the SandboxPool CR.
	DeleteMember(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError)
	// List enumerates SandboxPool CRs owned by envName (matched on
	// OwnerReferences).
	ListMembers(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError)
	// Get fetches one SandboxPool CR and verifies it is owned by envName
	// before projecting it.
	GetMember(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError)
}

// MemberPoolPatch is the editable subset of EnvClusterMember exposed to
// PUT /v1/sandboxenvs/{name}/sandboxpools/{poolName}. Pointer fields
// disambiguate "leave unchanged" from "explicit zero".
type MemberPoolPatch struct {
	Replicas    *int32
	MaxReplicas *int32
}

// New constructs the default Service implementation backed by the K8s
// client, the supplied PluginManager (nil = no plugins registered), and
// the InstanceType + Quota providers used to derive PoolName +
// ScalingGroup. Nil providers fall through to their Noop equivalents so
// unit tests can pass nils.
func New(
	c client.Client,
	pm *plugins.PluginManager,
	instProv instancetypeplugin.Provider,
	quotaProv quotaplugin.Provider,
) MemberPoolService {
	if instProv == nil {
		instProv = instancetypeplugin.NewNoop()
	}
	if quotaProv == nil {
		quotaProv = quotaplugin.NewNoop()
	}
	return &k8sService{client: c, pm: pm, instProv: instProv, quotaProv: quotaProv}
}

type k8sService struct {
	client    client.Client
	pm        *plugins.PluginManager
	instProv  instancetypeplugin.Provider
	quotaProv quotaplugin.Provider
}

// QuotaURLLabel is the label key carrying the quota identifier on a
// member. The QuotaProvider parses it via DeriveShortName when
// constructing the PoolName suffix.
const QuotaURLLabel = "quota.scitix.ai/url"

func (s *k8sService) AddMember(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	derived, appErr := derivePoolMember(ctx, s.instProv, s.quotaProv, envName, member)
	if appErr != nil {
		return nil, appErr
	}
	member = derived
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, key, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	for _, m := range envcommon.LocalClusterMembers(&env.Spec, localClusterID) {
		if m.Name == member.Name {
			return nil, domain.NewConflict(fmt.Sprintf("member pool %q already exists in env %q (derived from resources + quota)", member.Name, envName))
		}
	}

	candidate, appErr := s.renderCandidate(ctx, env, member)
	if appErr != nil {
		return nil, appErr
	}
	if err := poolrender.Validate(&candidate.Spec); err != nil {
		return nil, domain.NewBadRequest(err.Error())
	}
	// PreCreatePool is the only call site for this plugin hook — its
	// side-effects (Reservation Submit, scheduling labels, NodeAffinity,
	// …) and any mutations on candidate are captured into Member.Metadata
	// + Member.Spec so the Reconciler can materialise the Pool without
	// re-running plugin admission. The updated flag is informational here:
	// we snapshot Meta + Spec wholesale regardless, since this is the
	// first time the member is being created and there is no prior
	// snapshot to compare against.
	if _, appErr := s.pm.PreCreatePool(ctx, candidate); appErr != nil {
		return nil, appErr
	}
	member.Metadata = sanitizeMemberMetadata(candidate.ObjectMeta)
	member.Spec = *candidate.Spec.DeepCopy()

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		members := envcommon.LocalClusterMembers(&current.Spec, localClusterID)
		for _, m := range members {
			if m.Name == member.Name {
				return &domain.AppError{Code: domain.ErrCodeConflict,
					Message: fmt.Sprintf("member pool %q already exists in env %q", member.Name, envName)}
			}
		}
		envcommon.SetLocalClusterMembers(&current.Spec, localClusterID, append(members, member))
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

func (s *k8sService) UpdateMember(ctx context.Context, namespace, envName, poolName, localClusterID string, patch MemberPoolPatch) (*gen.SandboxPool, *domain.AppError) {
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
	members := envcommon.LocalClusterMembers(&env.Spec, localClusterID)
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
	if patch.Replicas != nil && scalingGroupHasAutoscaling(env, existing.Config.ScalingGroup) {
		return nil, domain.NewBadRequest(fmt.Sprintf("replicas is owned by the autoscaler for scalingGroup %q; only maxReplicas can be edited", existing.Config.ScalingGroup))
	}

	// Build the updated member by overlaying patch onto existing — preserves
	// Spec, Metadata, Config (other than Replicas / MaxReplicas), name.
	member := *existing.DeepCopy()
	if patch.Replicas != nil {
		member.Config.Replicas = *patch.Replicas
	}
	if patch.MaxReplicas != nil {
		v := *patch.MaxReplicas
		member.Config.MaxReplicas = &v
	}

	// Candidate Pool used by PreUpdatePool admission: start from the
	// frozen Member.Metadata + Member.Spec snapshot (not a fresh render —
	// Template upgrades do NOT auto-propagate). Overlay the patched
	// replica count so the plugin sees the new desired state.
	candidate := &agentsv1alpha1.SandboxPool{
		ObjectMeta: *member.Metadata.DeepCopy(),
		Spec:       *member.Spec.DeepCopy(),
	}
	candidate.Name = member.Name
	candidate.Namespace = env.Namespace
	candidate.Spec.Replicas = member.Config.Replicas
	before := candidate.DeepCopy()

	// Pod list (driver-supplied for PreUpdatePool) is left empty here; the
	// Reconciler still owns the in-cluster update path that supplies live
	// pods. This is a coarse pre-check at the API edge.
	pluginUpdated, appErr := s.pm.PreUpdatePool(ctx, candidate, nil)
	if appErr != nil {
		return nil, appErr
	}
	if pluginUpdated && !equality.Semantic.DeepEqual(before, candidate) {
		// Plugin really mutated something: persist the new snapshot back
		// onto the Member, then keep the live Pool aligned (if it exists)
		// so we don't lose the plugin's intent.
		member.Metadata = sanitizeMemberMetadata(candidate.ObjectMeta)
		member.Spec = *candidate.Spec.DeepCopy()
		if err := s.patchLivePoolFromMember(ctx, env.Namespace, &member); err != nil {
			return nil, domain.NewInternal(err.Error(), err)
		}
	} else {
		// No plugin mutation — propagate just the replica count into
		// Member.Spec so the Reconciler's drift detection observes it.
		member.Spec.Replicas = member.Config.Replicas
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		ms := envcommon.LocalClusterMembers(&current.Spec, localClusterID)
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
		envcommon.SetLocalClusterMembers(&current.Spec, localClusterID, ms)
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

func (s *k8sService) DeleteMember(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	// Run delete admission against the live Pool when present. Missing Pool
	// (Reconciler hasn't created it yet) means there's no plugin state to
	// release, so skip admission and proceed to the Env patch.
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err == nil {
		if envcommon.EnvNameFromOwnerRefs(pool.OwnerReferences) != envName {
			return nil, domain.NewNotFound(fmt.Sprintf("member pool %q not found in env %q", poolName, envName))
		}
		if _, appErr := s.pm.PreDeletePool(ctx, pool); appErr != nil {
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
		members := envcommon.LocalClusterMembers(&current.Spec, localClusterID)
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
		envcommon.SetLocalClusterMembers(&current.Spec, localClusterID, append([]agentsv1alpha1.EnvClusterMember(nil), out...))
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

func (s *k8sService) ListMembers(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: envName}, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	pools := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, pools, client.InNamespace(namespace)); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	items := make([]gen.SandboxPool, 0)
	for i := range pools.Items {
		p := &pools.Items[i]
		if envcommon.EnvNameFromOwnerRefs(p.OwnerReferences) != envName {
			continue
		}
		items = append(items, envcommon.PoolToGen(ctx, p))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *k8sService) GetMember(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if envcommon.EnvNameFromOwnerRefs(pool.OwnerReferences) != envName {
		return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
	}
	result := envcommon.PoolToGen(ctx, pool)
	return &result, nil
}

// renderCandidate builds the SandboxPool that plugin admission will see
// for an Add. It fetches the env's source SandboxTemplate, computes the
// imagePullSecret existence bit, and delegates to
// poolrender.RenderSandboxPool — exactly the same code path the
// Reconciler uses to materialise the eventual CR.
func (s *k8sService) renderCandidate(ctx context.Context, env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember) (*agentsv1alpha1.SandboxPool, *domain.AppError) {
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if env.Spec.TemplateRef.Name == "" {
		return nil, domain.NewBadRequest("env.spec.templateRef.name is empty")
	}
	if err := s.client.Get(ctx, client.ObjectKey{Name: env.Spec.TemplateRef.Name}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("source template %q not found", env.Spec.TemplateRef.Name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	ipsExists, _ := poolrender.ImagePullSecretExists(ctx, s.client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name))

	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:                   env,
		Template:              tmpl,
		Member:                member,
		ImagePullSecretExists: ipsExists,
	})
	if err != nil {
		return nil, domain.NewBadRequest(err.Error())
	}
	return pool, nil
}

// projectMemberPool returns the freshly materialised SandboxPool CR if it
// exists, else a minimal projection from the member fields. Used by
// Add/Update which return immediately after the Env MemberPoolPatch lands but before
// the Reconciler has had a chance to run.
func (s *k8sService) projectMemberPool(ctx context.Context, namespace, envName string, member agentsv1alpha1.EnvClusterMember) *gen.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: member.Name}, pool); err == nil &&
		envcommon.EnvNameFromOwnerRefs(pool.OwnerReferences) == envName {
		result := envcommon.PoolToGen(ctx, pool)
		return &result
	}
	return &gen.SandboxPool{
		Name:      member.Name,
		Namespace: namespace,
		Spec:      gen.SandboxPoolSpec{Replicas: member.Config.Replicas},
		OwningEnv: ptr.To(envName),
	}
}

// sanitizeMemberMetadata strips server-managed ObjectMeta fields from
// the post-PreCreatePool candidate so the snapshot stored on
// EnvClusterMember.Metadata only carries data that round-trips cleanly
// when the Reconciler materialises the live Pool. We keep
// Name/Namespace/Labels/Annotations/Finalizers (the user- and
// plugin-authored bits) and drop UID/ResourceVersion/Generation/CreationTimestamp/
// DeletionTimestamp/ManagedFields/OwnerReferences (server- or
// Reconciler-managed).
func sanitizeMemberMetadata(in metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        in.Name,
		Namespace:   in.Namespace,
		Labels:      cloneStringMap(in.Labels),
		Annotations: cloneStringMap(in.Annotations),
		Finalizers:  append([]string(nil), in.Finalizers...),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// patchLivePoolFromMember aligns the live SandboxPool CR with the
// frozen member.Metadata + member.Spec snapshot. Called after
// PreUpdatePool admits a mutation so the plugin's intent (Reservation
// resubmit, label change, ...) is applied without waiting for the next
// Reconciler tick. Replicas is intentionally taken from Config (the
// autoscaler-friendly source of truth) rather than from Spec.Replicas
// — they're equal here because UpdateMember just synchronised them.
//
// Returns nil when the Pool does not exist (Reconciler will pick up
// the new Member snapshot on first materialisation).
func (s *k8sService) patchLivePoolFromMember(ctx context.Context, namespace string, member *agentsv1alpha1.EnvClusterMember) error {
	live := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: member.Name}, live); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	base := live.DeepCopy()
	for k, v := range member.Metadata.Labels {
		if live.Labels == nil {
			live.Labels = map[string]string{}
		}
		live.Labels[k] = v
	}
	for k, v := range member.Metadata.Annotations {
		if live.Annotations == nil {
			live.Annotations = map[string]string{}
		}
		live.Annotations[k] = v
	}
	live.Spec.Replicas = member.Config.Replicas
	if equality.Semantic.DeepEqual(base, live) {
		return nil
	}
	return s.client.Patch(ctx, live, client.MergeFrom(base))
}

// Compile-time reference to corev1 — kept so the new sanitizer/import
// list stays stable when callers in this package start consuming pod
// shapes directly (UpdateMember's plugin admission is one such path).
var _ = corev1.Pod{}
