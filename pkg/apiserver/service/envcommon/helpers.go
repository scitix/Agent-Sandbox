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

// Package envcommon hosts the helpers shared between the env shell service
// (pkg/apiserver/service) and the per-resource sub-services (envmember,
// envautoscaler). It exists so the sub-services can be composed back into
// SandboxEnvService without an import cycle on the outer service package.
package envcommon

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// LocalClusterMembers returns a copy of the member list for the cluster
// segment matching localClusterID. An empty result includes both "segment
// absent" and "segment present with empty members".
func LocalClusterMembers(spec *agentsv1alpha1.SandboxEnvSpec, localClusterID string) []agentsv1alpha1.EnvClusterMember {
	if spec == nil {
		return nil
	}
	for _, c := range spec.Clusters {
		if c.ClusterID == localClusterID {
			return append([]agentsv1alpha1.EnvClusterMember(nil), c.Members...)
		}
	}
	return nil
}

// SetLocalClusterMembers replaces the Members slice on the cluster segment
// matching localClusterID, creating the segment when absent. Passing an
// empty members slice clears the local segment's members (the Reconciler
// then falls back to a single namesake Pool).
func SetLocalClusterMembers(spec *agentsv1alpha1.SandboxEnvSpec, localClusterID string, members []agentsv1alpha1.EnvClusterMember) {
	if spec == nil || localClusterID == "" {
		return
	}
	copyMembers := append([]agentsv1alpha1.EnvClusterMember(nil), members...)
	for i := range spec.Clusters {
		if spec.Clusters[i].ClusterID == localClusterID {
			spec.Clusters[i].Members = copyMembers
			return
		}
	}
	spec.Clusters = append(spec.Clusters, agentsv1alpha1.EnvClusterSpec{
		ClusterID: localClusterID,
		Members:   copyMembers,
	})
}

// EnvNameFromOwnerRefs returns the Env name from a Pool's owner
// references, or "" when the Pool is unowned (e.g. mid-adoption).
func EnvNameFromOwnerRefs(refs []metav1.OwnerReference) string {
	for _, ref := range refs {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind {
			return ref.Name
		}
	}
	return ""
}
