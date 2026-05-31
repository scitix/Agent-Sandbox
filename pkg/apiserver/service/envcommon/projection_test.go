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

// TestPoolToSummary_OmitsSpecYaml asserts the list projection drops the heavy
// SpecYaml field while keeping the fields the dashboard table reads, and that
// PoolToGen still carries SpecYaml for the Get/diff path.
func TestPoolToSummary_OmitsSpecYaml(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-2c8gi",
			Namespace: "default",
			Labels:    map[string]string{agentsv1alpha1.LabelScalingGroup: "2c8gi"},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 3},
	}

	summary := PoolToSummary(context.Background(), pool)
	if summary.Spec.Replicas != 3 {
		t.Errorf("summary Spec.Replicas = %d, want 3", summary.Spec.Replicas)
	}
	if summary.ScalingGroup == nil || *summary.ScalingGroup != "2c8gi" {
		t.Errorf("summary ScalingGroup = %v, want 2c8gi", summary.ScalingGroup)
	}

	// PoolToGen (Get path) keeps SpecYaml populated.
	full := PoolToGen(context.Background(), pool)
	if full.SpecYaml == nil || *full.SpecYaml == "" {
		t.Errorf("PoolToGen SpecYaml should be populated for the Get path")
	}
	if full.Spec.Replicas != summary.Spec.Replicas || full.ScalingGroup == nil {
		t.Errorf("PoolToGen should carry the same base fields as the summary")
	}
}
