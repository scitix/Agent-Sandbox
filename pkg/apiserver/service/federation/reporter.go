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

package federation

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// CapacitySource produces this cluster's current per-Env capacity, one record
// per (Env, scalingGroup) plus a whole-Env aggregate row.
type CapacitySource interface {
	Collect(ctx context.Context) ([]Capacity, error)
}

// k8sCapacitySource derives capacity from the locally-reconciled SandboxEnv
// statuses. The Env reconciler already aggregates per-member idle/running/
// desired into status, so this source only reads it back.
type k8sCapacitySource struct {
	client         client.Client
	localClusterID string
	now            func() time.Time
}

// NewCapacitySource builds a CapacitySource backed by the SandboxEnv informer
// cache.
func NewCapacitySource(c client.Client, localClusterID string) CapacitySource {
	return &k8sCapacitySource{client: c, localClusterID: localClusterID, now: time.Now}
}

func (s *k8sCapacitySource) Collect(ctx context.Context) ([]Capacity, error) {
	var envs agentsv1alpha1.SandboxEnvList
	if err := s.client.List(ctx, &envs); err != nil {
		return nil, err
	}
	now := s.now()
	var out []Capacity
	for i := range envs.Items {
		env := &envs.Items[i]
		// The Env reconciler already stamps ScalingGroup + AutoscalingEnabled +
		// ScaleUpHeadroom onto each local observed member, so this source reads
		// them straight back — no re-derivation from spec needed.
		for ci := range env.Status.Clusters {
			cs := &env.Status.Clusters[ci]
			if !cs.IsLocal {
				continue
			}
			for mi := range cs.ObservedMembers {
				m := &cs.ObservedMembers[mi]
				out = append(out, Capacity{
					ClusterID:          s.localClusterID,
					Namespace:          env.Namespace,
					EnvName:            env.Name,
					MemberPool:         m.Name,
					ScalingGroup:       m.ScalingGroup,
					Idle:               m.IdleCount,
					Running:            m.RunningCount,
					Desired:            m.DesiredReplicas,
					Pending:            m.PendingRequests,
					AutoscalingEnabled: m.AutoscalingEnabled,
					Capacity:           headroomOf(m),
					ObservedAt:         now,
				})
			}
		}
	}
	return out, nil
}

// headroomOf maps an observed member's ScaleUpHeadroom pointer onto the
// federation Capacity convention: 0 when autoscaling is off, -1 (unbounded)
// when enabled with no finite ceiling (nil pointer while enabled), else the
// finite estimate (0 = at ceiling).
func headroomOf(m *agentsv1alpha1.EnvObservedMember) int32 {
	if !m.AutoscalingEnabled {
		return 0
	}
	if m.ScaleUpHeadroom == nil {
		return -1
	}
	return *m.ScaleUpHeadroom
}
