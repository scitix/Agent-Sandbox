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
	"fmt"

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
//     The server resolves it via the provider, persists the catalog name +
//     multiplier on the member, AND stamps the resolved resources into
//     Config.InlineResources so the renderer (which still consumes
//     InlineResources in Phase 1) sees a consistent picture even before the
//     catalog-driven renderer lands in Phase 2.
//   - Otherwise the caller must supply Config.InlineResources directly.
//   - Both paths empty → BadRequest.
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
		resolved, appErr := instProv.Resolve(ctx, cfg.InstanceType, mult)
		if appErr != nil {
			return in, appErr
		}
		resources = resolved
		cfg.Multiplier = mult
		// Stamp the resolved resources onto InlineResources so the Phase 1
		// renderer (which consumes InlineResources) sees the catalog's
		// picture. InstanceType + Multiplier are preserved so when the
		// catalog-aware renderer ships in Phase 2 it can re-derive on
		// catalog updates without needing API rewrites.
		req := resolved.DeepCopy()
		cfg.InlineResources = req
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
