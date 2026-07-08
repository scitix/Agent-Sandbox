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

package envmember

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// derivePoolMember resolves the supplied member input to a fully populated
// EnvClusterMember: it picks instanceType or inlineResources, computes the
// effective resources, derives the ScalingGroup name and the PoolName, and
// fills both onto the returned value.
//
// Naming rules:
//
//	PoolName     = envName + "-" + resourceKey [+ "-" + quotaShort]
//	ScalingGroup = resourceKey  (e.g. "2c8Gi")
//
// where resourceKey = instancetype.DeriveResourceKey(effective) and
// quotaShort = quotaProv.DeriveShortName(labels[QuotaURLLabel]). The quota
// suffix is omitted when the provider returns "" or when no quota label is
// present.
//
// Validation:
//   - When the InstanceType catalog is enabled, prefer Config.InstanceType.
//     `instanceType × multiplier` defines the reservation "envelope". If the
//     caller ALSO supplies Config.InlineResources, those are treated as the
//     actual (possibly rounded-down) Pod request and validated to fit within
//     the envelope in every dimension (see instancetype.FitsWithin) — a Pod may
//     request less than the reserved instance but never more. If InlineResources
//     is absent, the Pod resources default to the full envelope.
//     The real multiplier is stamped into the reservation-replica-quota
//     annotation so the SI Scheduler reservation plugin charges quota per whole
//     instance even when the Pod request is rounded down.
//   - Otherwise the caller must supply Config.InlineResources directly.
//   - Both paths empty → BadRequest.
//
// Grouping: ScalingGroup / PoolName are derived from the effective Pod request
// (the rounded-down InlineResources when downsized, else the full envelope), so
// the name reflects the Pod's real size (e.g. "1c2gi") and Pools downsized
// differently land in distinct scaling groups.
//
// envName is checked against the 24-char limit (matches openapi.yaml) so
// the composed PoolName stays under the 63-char DNS-label cap.
func derivePoolMember(
	ctx context.Context,
	instProv instancetypeplugin.Provider,
	quotaProv quotaplugin.Provider,
	envName string,
	in agentsv1alpha1.EnvClusterMember,
) (agentsv1alpha1.EnvClusterMember, *domain.AppError) {
	if len(envName) > 24 {
		return in, domain.NewBadRequest(fmt.Sprintf("env name %q exceeds 24-char cap", envName))
	}

	cfg := &in.Config
	useInstanceType := instProv != nil && instProv.Enabled() && cfg.InstanceType != ""
	var resources corev1.ResourceRequirements
	switch {
	case useInstanceType:
		mult := max(cfg.Multiplier, 1)
		envelope, appErr := instProv.Resolve(ctx, cfg.InstanceType, mult)
		if appErr != nil {
			return in, appErr
		}
		cfg.Multiplier = mult

		if cfg.InlineResources != nil {
			// Rounded-down real request: every dimension must fit within the
			// envelope (round down allowed, round up rejected).
			if dim, ok := instancetypeplugin.FitsWithin(cfg.InlineResources.Requests, envelope.Requests); !ok {
				return in, domain.NewBadRequest(fmt.Sprintf(
					"inlineResources request %q exceeds instanceType %q × multiplier %d",
					dim, cfg.InstanceType, mult))
			}
			if dim, ok := instancetypeplugin.FitsWithin(cfg.InlineResources.Limits, envelope.Requests); !ok {
				return in, domain.NewBadRequest(fmt.Sprintf(
					"inlineResources limit %q exceeds instanceType %q × multiplier %d",
					dim, cfg.InstanceType, mult))
			}
			// Keep the caller's (smaller) request as the actual Pod resources.
		} else {
			// No downsizing: the Pod uses the full envelope.
			cfg.InlineResources = envelope.DeepCopy()
		}

		// Group/name by the ACTUAL request so a downsized Pool surfaces its real
		// size (e.g. "1c2gi"), not the reserved envelope. cfg.InlineResources now
		// holds the effective request in both branches (caller-supplied when
		// downsized, else the full envelope). Two Pools of the same instance
		// downsized differently are genuinely different effective sizes, so they
		// belong to different scaling groups.
		resources = *cfg.InlineResources

		// Carry the real multiplier to the reservation plugin (charges quota
		// per whole instance even when the request is rounded down).
		if appErr := stampReservationReplicaQuota(cfg, cfg.InstanceType, mult); appErr != nil {
			return in, appErr
		}
	case cfg.InlineResources != nil:
		resources = *cfg.InlineResources
		cfg.InstanceType = ""
		cfg.Multiplier = 0
	default:
		return in, domain.NewBadRequest("one of instanceType or inlineResources must be supplied")
	}

	resourceKey := instancetypeplugin.DeriveResourceKey(resources)
	if resourceKey == "" || resourceKey == "default" {
		return in, domain.NewBadRequest("could not derive a non-empty ResourceKey from the supplied resources")
	}

	suffix := ""
	if quotaProv != nil && quotaProv.Enabled() {
		if quotaID, ok := cfg.Labels[QuotaURLLabel]; ok && quotaID != "" {
			if short := quotaProv.DeriveShortName(quotaID); short != "" {
				suffix = "-" + short
			}
		}
	}

	in.Name = envName + "-" + resourceKey + suffix
	cfg.ScalingGroup = resourceKey
	return in, nil
}

// stampReservationReplicaQuota writes {"<instanceType>":"<multiplier>"} into
// Config.Annotations under AnnotationReservationReplicaQuota. The renderer
// copies Config.Annotations onto the Pool after template-annotation sync, so
// this value overrides any template-authored placeholder and carries the real
// whole-instance count to the SI Scheduler reservation plugin. This lets the
// reservation charge quota per whole instance even when the Pod's actual
// request has been rounded down below the instance size.
func stampReservationReplicaQuota(cfg *agentsv1alpha1.EnvClusterMemberConfig, instanceType string, mult int32) *domain.AppError {
	b, err := json.Marshal(map[string]string{instanceType: strconv.FormatInt(int64(mult), 10)})
	if err != nil {
		return domain.NewInternal("encode reservation replica quota annotation", err)
	}
	if cfg.Annotations == nil {
		cfg.Annotations = map[string]string{}
	}
	cfg.Annotations[agentsv1alpha1.AnnotationReservationReplicaQuota] = string(b)
	return nil
}

// scalingGroupHasAutoscaling reports whether autoscaling is enabled for
// the supplied group name on env. Returns true only when a group with
// that name exists AND its own Enabled=true. Used by Update to decide
// whether `replicas` is editable (it isn't when the autoscaler owns it
// for this group).
func scalingGroupHasAutoscaling(env *agentsv1alpha1.SandboxEnv, groupName string) bool {
	if env == nil || env.Spec.Autoscaling == nil || groupName == "" {
		return false
	}
	for _, g := range env.Spec.Autoscaling.Groups {
		if g.Name == groupName {
			return g.Enabled
		}
	}
	return false
}

// findEnabledScalingGroup returns a pointer to the autoscaling group
// matching groupName when the group exists AND is enabled. Returns nil
// otherwise — manual-replicas mode, group absent, or group disabled.
// The pointer is into env.Spec.Autoscaling.Groups; callers must not
// mutate through it.
func findEnabledScalingGroup(env *agentsv1alpha1.SandboxEnv, groupName string) *agentsv1alpha1.EnvAutoscalingGroup {
	if env == nil || env.Spec.Autoscaling == nil || groupName == "" {
		return nil
	}
	for i := range env.Spec.Autoscaling.Groups {
		g := &env.Spec.Autoscaling.Groups[i]
		if g.Name == groupName && g.Enabled {
			return g
		}
	}
	return nil
}

// ensureScalingGroup appends a minimal EnvAutoscalingGroup named groupName
// to spec when none exists yet, so every member's ScalingGroup always has a
// matching group entry. The group starts disabled (manual replicas); its
// MinReplicas/MaxReplicas and policy fields are filled by the K8s API
// server's CRD defaulting when the Env is patched, so no code defaults are
// hidden here. Empty groupName is a no-op (member excluded from
// autoscaling). Returns true when a group was added.
//
// The Env reconciler's group GC (reconcileScalingGroups) is the counterpart
// that removes groups once no member references them.
func ensureScalingGroup(spec *agentsv1alpha1.SandboxEnvSpec, groupName string) bool {
	if spec == nil || groupName == "" {
		return false
	}
	if spec.Autoscaling == nil {
		spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{}
	}
	for i := range spec.Autoscaling.Groups {
		if spec.Autoscaling.Groups[i].Name == groupName {
			return false
		}
	}
	spec.Autoscaling.Groups = append(spec.Autoscaling.Groups, agentsv1alpha1.EnvAutoscalingGroup{Name: groupName})
	return true
}

// sumGroupReplicas adds up the spec.replicas of every member in
// localClusterID whose Config.ScalingGroup matches groupName, excluding
// the member whose name equals excludeName (used to omit self when
// computing "other siblings' total"). Set excludeName to "" to count
// every member.
func sumGroupReplicas(env *agentsv1alpha1.SandboxEnv, localClusterID, groupName, excludeName string) int32 {
	if env == nil || groupName == "" {
		return 0
	}
	var total int32
	for ci := range env.Spec.Clusters {
		if env.Spec.Clusters[ci].ClusterID != localClusterID {
			continue
		}
		for mi := range env.Spec.Clusters[ci].Members {
			m := &env.Spec.Clusters[ci].Members[mi]
			if m.Config.ScalingGroup != groupName {
				continue
			}
			if m.Name == excludeName {
				continue
			}
			total += m.Spec.Replicas
		}
	}
	return total
}
