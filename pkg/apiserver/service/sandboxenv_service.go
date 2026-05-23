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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
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
	// UpdateAutoscaling replaces env.Spec.Autoscaling with the supplied value.
	// The rest of the Env spec is read-only at this layer.
	UpdateAutoscaling(ctx context.Context, input UpdateEnvAutoscalingInput) (*gen.SandboxEnv, *domain.AppError)
}

// UpdateEnvAutoscalingInput carries the fields UpdateAutoscaling reads.
//
// Autoscaling==nil means "clear the autoscaling spec" — pass an empty value
// (e.g. {Enabled: false}) when the caller wants to disable rather than
// remove the configuration.
type UpdateEnvAutoscalingInput struct {
	Name        string
	Namespace   string
	Autoscaling *gen.EnvAutoscalingSpec
}

type k8sSandboxEnvService struct {
	client client.Client
}

// NewSandboxEnvService constructs the default service implementation.
func NewSandboxEnvService(c client.Client) SandboxEnvService {
	return &k8sSandboxEnvService{client: c}
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
	return &result, nil
}

// UpdateAutoscaling persists a new autoscaling block onto the Env.
//
// Idempotency: the caller may submit identical autoscaling repeatedly; the
// Patch is a no-op when the JSON-serialised spec is unchanged. Conflicts on
// the Env Generation are retried automatically.
//
// Validation: each group's Name must be non-empty, Mode (when set) must be
// one of the known scale-up modes. Other field constraints are enforced by
// the CRD's OpenAPI validation when the Patch reaches the apiserver.
func (s *k8sSandboxEnvService) UpdateAutoscaling(ctx context.Context, input UpdateEnvAutoscalingInput) (*gen.SandboxEnv, *domain.AppError) {
	if appErr := validateEnvAutoscaling(input.Autoscaling); appErr != nil {
		return nil, appErr
	}
	key := types.NamespacedName{Namespace: input.Namespace, Name: input.Name}
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, key, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", input.Name, input.Namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		current.Spec.Autoscaling = autoscalingFromGen(input.Autoscaling)
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	// Re-read so the response reflects the persisted state (status fields are
	// populated by the controller, so the result also includes anything that
	// reconciled while we were patching).
	if err := s.client.Get(ctx, key, env); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}
	result := envToGen(env)
	return &result, nil
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
