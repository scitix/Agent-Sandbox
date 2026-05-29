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

package envcommon

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// TestPoolToGen_ScalingGroupFromLabel asserts the scaling-group label is
// surfaced on the wire shape, and absent label projects to an empty string.
func TestPoolToGen_ScalingGroupFromLabel(t *testing.T) {
	withLabel := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-2c8gi",
			Namespace: "default",
			Labels:    map[string]string{agentsv1alpha1.LabelScalingGroup: "2c8gi"},
		},
	}
	got := PoolToGen(context.Background(), withLabel)
	if got.ScalingGroup == nil || *got.ScalingGroup != "2c8gi" {
		t.Fatalf("ScalingGroup = %v, want %q", got.ScalingGroup, "2c8gi")
	}

	bare := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "env-a-bare", Namespace: "default"},
	}
	gotBare := PoolToGen(context.Background(), bare)
	if gotBare.ScalingGroup == nil || *gotBare.ScalingGroup != "" {
		t.Fatalf("ScalingGroup for unlabelled pool = %v, want empty string", gotBare.ScalingGroup)
	}
}
