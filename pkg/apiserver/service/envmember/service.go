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
// SandboxEnv K8s client but talk to plugins via the PoolAdmitter
// abstraction owned by the parent service package.
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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// Service is the Env-scoped Pool CRUD surface. Routes onto
// `/v1/sandboxenvs/{name}/sandboxpools/*` via handlers/server.go.
type Service interface {
	// Add appends a new member to env's local cluster segment. The server
	// derives member.Name and member.ScalingGroup from the supplied
	// resources + quota label; any caller-supplied values for these fields
	// are overwritten. Plugin admission (PreCreatePool) runs against the
	// fully rendered prospective SandboxPool before the Env patch lands.
	Add(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError)
	// Update adjusts the named member's replica counts. Resource shape,
	// instanceType, labels, and annotations are immutable post-create —
	// callers must Delete + Add to change them. When the member's
	// ScalingGroup has autoscaling enabled, Replicas is rejected.
	Update(ctx context.Context, namespace, envName, poolName, localClusterID string, patch Patch) (*gen.SandboxPool, *domain.AppError)
	// Delete removes the named member from env's local cluster segment.
	// The Reconciler cascade-deletes the SandboxPool CR.
	Delete(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError)
	// List enumerates SandboxPool CRs owned by envName (matched on
	// OwnerReferences).
	List(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError)
	// Get fetches one SandboxPool CR and verifies it is owned by envName
	// before projecting it.
	Get(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError)
}

// Patch is the editable subset of EnvClusterMember exposed to
// PUT /v1/sandboxenvs/{name}/sandboxpools/{poolName}. Pointer fields
// disambiguate "leave unchanged" from "explicit zero".
type Patch struct {
	Replicas    *int32
	MaxReplicas *int32
}

// New constructs the default Service implementation backed by the K8s
// client, the supplied PoolAdmitter, and the InstanceType + Quota
// providers used to derive PoolName + ScalingGroup. A nil admitter is
// treated as service.NoOpPoolAdmitter; nil providers fall through to
// their Noop equivalents so unit tests can pass nils.
func New(
	c client.Client,
	admitter service.PoolAdmitter,
	instProv instancetypeplugin.Provider,
	quotaProv quotaplugin.Provider,
) Service {
	if admitter == nil {
		admitter = service.NoOpPoolAdmitter{}
	}
	if instProv == nil {
		instProv = instancetypeplugin.NewNoop()
	}
	if quotaProv == nil {
		quotaProv = quotaplugin.NewNoop()
	}
	return &k8sService{client: c, admitter: admitter, instProv: instProv, quotaProv: quotaProv}
}

type k8sService struct {
	client    client.Client
	admitter  service.PoolAdmitter
	instProv  instancetypeplugin.Provider
	quotaProv quotaplugin.Provider
}

// QuotaURLLabel is the label key carrying the quota identifier on a
// member. The QuotaProvider parses it via DeriveShortName when
// constructing the PoolName suffix.
const QuotaURLLabel = "quota.scitix.ai/url"

func (s *k8sService) Add(ctx context.Context, namespace, envName, localClusterID string, member agentsv1alpha1.EnvClusterMember) (*gen.SandboxPool, *domain.AppError) {
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
	for _, m := range service.LocalClusterMembers(&env.Spec, localClusterID) {
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
		members := service.LocalClusterMembers(&current.Spec, localClusterID)
		for _, m := range members {
			if m.Name == member.Name {
				return &domain.AppError{Code: domain.ErrCodeConflict,
					Message: fmt.Sprintf("member pool %q already exists in env %q", member.Name, envName)}
			}
		}
		service.SetLocalClusterMembers(&current.Spec, localClusterID, append(members, member))
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

func (s *k8sService) Update(ctx context.Context, namespace, envName, poolName, localClusterID string, patch Patch) (*gen.SandboxPool, *domain.AppError) {
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
	members := service.LocalClusterMembers(&env.Spec, localClusterID)
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

	// Build the updated member by overlaying patch onto existing — preserves
	// resources, labels, annotations, name, scalingGroup.
	member := *existing
	if patch.Replicas != nil {
		member.Replicas = *patch.Replicas
	}
	if patch.MaxReplicas != nil {
		v := *patch.MaxReplicas
		member.MaxReplicas = &v
	}

	candidate, appErr := s.candidateForUpdate(ctx, env, member)
	if appErr != nil {
		return nil, appErr
	}
	if err := poolrender.Validate(&candidate.Spec); err != nil {
		return nil, domain.NewBadRequest(err.Error())
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
		ms := service.LocalClusterMembers(&current.Spec, localClusterID)
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
		service.SetLocalClusterMembers(&current.Spec, localClusterID, ms)
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

func (s *k8sService) Delete(ctx context.Context, namespace, envName, poolName, localClusterID string) (*gen.DeleteSandboxPoolResult, *domain.AppError) {
	if localClusterID == "" {
		return nil, domain.NewServiceUnavailable("server misconfigured: LOCAL_CLUSTER_ID not set")
	}
	key := types.NamespacedName{Namespace: namespace, Name: envName}

	// Run delete admission against the live Pool when present. Missing Pool
	// (Reconciler hasn't created it yet) means there's no plugin state to
	// release, so skip admission and proceed to the Env patch.
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err == nil {
		if service.EnvNameFromOwnerRefs(pool.OwnerReferences) != envName {
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
		members := service.LocalClusterMembers(&current.Spec, localClusterID)
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
		service.SetLocalClusterMembers(&current.Spec, localClusterID, append([]agentsv1alpha1.EnvClusterMember(nil), out...))
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

func (s *k8sService) List(ctx context.Context, namespace, envName string) ([]gen.SandboxPool, *domain.AppError) {
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
		if service.EnvNameFromOwnerRefs(p.OwnerReferences) != envName {
			continue
		}
		items = append(items, service.PoolToGen(ctx, p, nil))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *k8sService) Get(ctx context.Context, namespace, envName, poolName string) (*gen.SandboxPool, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: poolName}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	if service.EnvNameFromOwnerRefs(pool.OwnerReferences) != envName {
		return nil, domain.NewNotFound(fmt.Sprintf("sandbox pool %q not found in env %q", poolName, envName))
	}
	result := service.PoolToGen(ctx, pool, nil)
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

// candidateForUpdate produces the prospective SandboxPool for an Update.
// When the live Pool exists, the result is the live object with the
// updated member overlaid; otherwise it falls back to renderCandidate.
//
// The live-Pool overlay path lets PreUpdatePool plugins (e.g. quota delta
// reservation) compare new vs. old replica counts directly. The
// fresh-render fallback handles the window between a successful Add and
// the Reconciler materialising the CR — admission still gets a full
// picture of resources × replicas.
func (s *k8sService) candidateForUpdate(ctx context.Context, env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember) (*agentsv1alpha1.SandboxPool, *domain.AppError) {
	live := &agentsv1alpha1.SandboxPool{}
	err := s.client.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: member.Name}, live)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return s.renderCandidate(ctx, env, member)
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	overlay := live.DeepCopy()
	overlay.Spec.Replicas = member.Replicas
	// Per-member labels / annotations are immutable post-create, but if a
	// plugin set them at admission time previously the overlay still
	// surfaces the latest member state for admission.
	if len(member.Labels) > 0 {
		if overlay.Labels == nil {
			overlay.Labels = map[string]string{}
		}
		maps.Copy(overlay.Labels, member.Labels)
	}
	if len(member.Annotations) > 0 {
		if overlay.Annotations == nil {
			overlay.Annotations = map[string]string{}
		}
		maps.Copy(overlay.Annotations, member.Annotations)
	}
	return overlay, nil
}

// projectMemberPool returns the freshly materialised SandboxPool CR if it
// exists, else a minimal projection from the member fields. Used by
// Add/Update which return immediately after the Env Patch lands but before
// the Reconciler has had a chance to run.
func (s *k8sService) projectMemberPool(ctx context.Context, namespace, envName string, member agentsv1alpha1.EnvClusterMember) *gen.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: member.Name}, pool); err == nil &&
		service.EnvNameFromOwnerRefs(pool.OwnerReferences) == envName {
		result := service.PoolToGen(ctx, pool, nil)
		return &result
	}
	return &gen.SandboxPool{
		Name:      member.Name,
		Namespace: namespace,
		Spec:      gen.SandboxPoolSpec{Replicas: member.Replicas},
		OwningEnv: ptr.To(envName),
	}
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
