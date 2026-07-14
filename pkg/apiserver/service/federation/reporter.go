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

		// Whole-Env aggregate row (group == "").
		out = append(out, Capacity{
			ClusterID:    s.localClusterID,
			Namespace:    env.Namespace,
			EnvName:      env.Name,
			ScalingGroup: "",
			Idle:         env.Status.IdleReplicas,
			Running:      env.Status.RunningReplicas,
			Desired:      env.Status.DesiredReplicas,
			Pending:      localPending(env),
			Capacity:     -1,
			ObservedAt:   now,
		})

		// One row per scaling group.
		for _, g := range env.Status.ScalingGroups {
			out = append(out, Capacity{
				ClusterID:    s.localClusterID,
				Namespace:    env.Namespace,
				EnvName:      env.Name,
				ScalingGroup: g.Name,
				Idle:         g.TotalIdle,
				Running:      g.TotalRunning,
				Desired:      g.TotalDesired,
				Capacity:     -1,
				ObservedAt:   now,
			})
		}
	}
	return out, nil
}

// localPending sums the claim-queue length across the local cluster segment's
// observed members.
func localPending(env *agentsv1alpha1.SandboxEnv) int32 {
	total := int32(0)
	for _, c := range env.Status.Clusters {
		if !c.IsLocal {
			continue
		}
		for _, m := range c.ObservedMembers {
			total += m.PendingRequests
		}
	}
	return total
}
