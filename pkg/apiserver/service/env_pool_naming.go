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

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// QuotaURLLabel is the label key carrying the quota identifier on a member.
// The QuotaProvider parses it via DeriveShortName when constructing the
// PoolName suffix.
const QuotaURLLabel = "quota.scitix.ai/url"

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
//   - When the InstanceType catalog is enabled, prefer m.InstanceType. The
//     server resolves it via the provider and persists the catalog name +
//     multiplier on the member (InlineResources is cleared so the Reconciler
//     can keep deriving from the catalog at render time).
//   - Otherwise the caller must supply InlineResources directly.
//   - Both paths empty → BadRequest.
//
// envName is checked against the 24-char limit (matches openapi.yaml) so the
// composed PoolName stays under the 63-char DNS-label cap.
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

	useInstanceType := instProv != nil && instProv.Enabled() && in.InstanceType != ""
	var resources corev1.ResourceRequirements
	switch {
	case useInstanceType:
		mult := max(in.Multiplier, 1)
		resolved, appErr := instProv.Resolve(ctx, in.InstanceType, mult)
		if appErr != nil {
			return in, appErr
		}
		resources = resolved
		in.Multiplier = mult
		// Catalog path persists InstanceType + Multiplier on the member —
		// the Reconciler re-resolves resources at render time so catalog
		// updates flow through without needing API rewrites.
		in.InlineResources = nil
	case in.InlineResources != nil:
		resources = *in.InlineResources
		in.InstanceType = ""
		in.Multiplier = 0
	default:
		return in, domain.NewBadRequest("one of instanceType or inlineResources must be supplied")
	}

	resourceKey := instancetypeplugin.DeriveResourceKey(resources)
	if resourceKey == "" || resourceKey == "default" {
		return in, domain.NewBadRequest("could not derive a non-empty ResourceKey from the supplied resources")
	}

	suffix := ""
	if quotaProv != nil && quotaProv.Enabled() {
		if quotaID, ok := in.Labels[QuotaURLLabel]; ok && quotaID != "" {
			if short := quotaProv.DeriveShortName(quotaID); short != "" {
				suffix = "-" + short
			}
		}
	}

	in.Name = envName + "-" + resourceKey + suffix
	in.ScalingGroup = resourceKey
	return in, nil
}

// scalingGroupHasAutoscaling reports whether autoscaling is enabled for the
// supplied group name on env. Returns true only when env.spec.autoscaling is
// enabled AND a group with that name exists. Used by UpdateMemberPool to
// decide whether `replicas` is editable (it isn't when autoscaling owns it).
func scalingGroupHasAutoscaling(env *agentsv1alpha1.SandboxEnv, groupName string) bool {
	if env == nil || env.Spec.Autoscaling == nil || !env.Spec.Autoscaling.Enabled || groupName == "" {
		return false
	}
	for _, g := range env.Spec.Autoscaling.Groups {
		if g.Name == groupName {
			return true
		}
	}
	return false
}
