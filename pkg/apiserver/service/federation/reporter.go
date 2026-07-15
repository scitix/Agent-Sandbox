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
		// Member name → scaling group, from the local cluster's spec segment.
		// ObservedMember does not carry the group, so join it from config.
		groupOf := memberScalingGroups(env, s.localClusterID)
		for ci := range env.Status.Clusters {
			cs := &env.Status.Clusters[ci]
			if !cs.IsLocal {
				continue
			}
			for mi := range cs.ObservedMembers {
				m := &cs.ObservedMembers[mi]
				out = append(out, Capacity{
					ClusterID:    s.localClusterID,
					Namespace:    env.Namespace,
					EnvName:      env.Name,
					MemberPool:   m.Name,
					ScalingGroup: groupOf[m.Name],
					Idle:         m.IdleCount,
					Running:      m.RunningCount,
					Desired:      m.DesiredReplicas,
					Pending:      m.PendingRequests,
					Capacity:     -1,
					ObservedAt:   now,
				})
			}
		}
	}
	return out, nil
}

// memberScalingGroups maps member pool name → scaling group for the local
// cluster's spec segment.
func memberScalingGroups(env *agentsv1alpha1.SandboxEnv, localClusterID string) map[string]string {
	out := map[string]string{}
	for ci := range env.Spec.Clusters {
		cs := &env.Spec.Clusters[ci]
		if cs.ClusterID != localClusterID {
			continue
		}
		for mi := range cs.Members {
			out[cs.Members[mi].Name] = cs.Members[mi].Config.ScalingGroup
		}
	}
	return out
}
