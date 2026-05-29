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

package envautoscaler

import (
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// SpecToGen projects the CRD EnvAutoscalingSpec into the wire shape. nil
// CRD value yields nil gen pointer so callers can distinguish "never
// configured" from "configured with no groups". The handler / list path
// normalises nil to a zero value for client-side ergonomics.
func SpecToGen(a *agentsv1alpha1.EnvAutoscalingSpec) *gen.EnvAutoscalingSpec {
	if a == nil {
		return nil
	}
	out := &gen.EnvAutoscalingSpec{}
	if len(a.Groups) > 0 {
		groups := make([]gen.EnvAutoscalingGroup, 0, len(a.Groups))
		for _, g := range a.Groups {
			groups = append(groups, GroupToGen(g))
		}
		out.Groups = &groups
	}
	return out
}

// GroupToGen projects one CRD EnvAutoscalingGroup into the wire shape.
// ScaleUpPolicy and ScaleDownPolicy are CRD value types and always
// project to non-nil wire pointers — the wire format still uses
// pointers to preserve the "field not sent" semantic, but the CR side
// has nothing to omit.
func GroupToGen(g agentsv1alpha1.EnvAutoscalingGroup) gen.EnvAutoscalingGroup {
	out := gen.EnvAutoscalingGroup{Name: g.Name, Enabled: ptr.To(g.Enabled)}
	if g.MinReplicas != nil {
		out.MinReplicas = ptr.To(*g.MinReplicas)
	}
	if g.MaxReplicas != nil {
		out.MaxReplicas = ptr.To(*g.MaxReplicas)
	}
	out.ScaleUpPolicy = scaleUpPolicyToGen(g.ScaleUpPolicy)
	out.ScaleDownPolicy = scaleDownPolicyToGen(g.ScaleDownPolicy)
	return out
}

func scaleUpPolicyToGen(p agentsv1alpha1.PoolScaleUpPolicy) *gen.PoolScaleUpPolicy {
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
	if p.IdleZeroQuietWindowSeconds > 0 {
		out.IdleZeroQuietWindowSeconds = ptr.To(p.IdleZeroQuietWindowSeconds)
	}
	if p.SaturationCooldownSeconds > 0 {
		out.SaturationCooldownSeconds = ptr.To(p.SaturationCooldownSeconds)
	}
	return out
}

func scaleDownPolicyToGen(p agentsv1alpha1.PoolScaleDownPolicy) *gen.PoolScaleDownPolicy {
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

func scaleUpPolicyFromGen(p *gen.PoolScaleUpPolicy) agentsv1alpha1.PoolScaleUpPolicy {
	out := agentsv1alpha1.PoolScaleUpPolicy{}
	if p == nil {
		return out
	}
	if p.Mode != nil {
		out.Mode = agentsv1alpha1.PoolScaleUpMode(*p.Mode)
	}
	if p.CooldownSeconds != nil {
		out.CooldownSeconds = *p.CooldownSeconds
	}
	if p.IdleThresholdSeconds != nil {
		out.IdleThresholdSeconds = *p.IdleThresholdSeconds
	}
	if p.IdleZeroQuietWindowSeconds != nil {
		out.IdleZeroQuietWindowSeconds = *p.IdleZeroQuietWindowSeconds
	}
	if p.SaturationCooldownSeconds != nil {
		out.SaturationCooldownSeconds = *p.SaturationCooldownSeconds
	}
	return out
}

func scaleDownPolicyFromGen(p *gen.PoolScaleDownPolicy) agentsv1alpha1.PoolScaleDownPolicy {
	out := agentsv1alpha1.PoolScaleDownPolicy{}
	if p == nil {
		return out
	}
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
