// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0
package poolrender_test

import (
	"reflect"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

func TestRenderFrozenInstancePlacementSurvivesTemplateRefresh(t *testing.T) {
	env, tmpl := newTestEnv(), newTestTemplate()
	member := agentsv1alpha1.EnvClusterMember{Name: "env-a-8c16gi-1gpu", Metadata: agentsv1alpha1.MemberMetadata{Annotations: map[string]string{instancetype.NodeSelectorSnapshotAnnotation: `{"example.com/model":"model-a"}`}}, Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "8c16gi-1gpu"}}
	tmpl.Spec.Template.Spec.NodeSelector = map[string]string{"example.com/zone": "a"}
	first, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: member})
	if err != nil {
		t.Fatal(err)
	}
	// A later template revision has a different general placement constraint.
	tmpl.Spec.Template.Spec.NodeSelector = map[string]string{"example.com/zone": "b"}
	tmpl.Spec.Version = "next"
	second, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: member})
	if err != nil {
		t.Fatal(err)
	}
	if second.Spec.Template.Spec.NodeSelector["example.com/model"] != "model-a" || second.Spec.Template.Spec.NodeSelector["example.com/zone"] != "b" {
		t.Fatal("refresh lost frozen selector or retained stale template selector")
	}
	if first.Annotations[instancetype.NodeSelectorSnapshotAnnotation] != second.Annotations[instancetype.NodeSelectorSnapshotAnnotation] {
		t.Fatal("snapshot drifted")
	}
	if second.Name != member.Name || member.Config.ScalingGroup != "8c16gi-1gpu" {
		t.Fatal("member identity changed")
	}
}

func TestRenderRejectsForgedConfigAndIgnoresTemplateSnapshot(t *testing.T) {
	env, tmpl := newTestEnv(), newTestTemplate()
	tmpl.Annotations[instancetype.NodeSelectorSnapshotAnnotation] = `{"example.com/model":"forged"}`
	member := agentsv1alpha1.EnvClusterMember{Name: "legacy", Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "8c16gi"}}
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: member})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Spec.Template.Spec.NodeSelector) != 0 {
		t.Fatal("legacy member was retroactively constrained")
	}
	if _, ok := pool.Annotations[instancetype.NodeSelectorSnapshotAnnotation]; ok {
		t.Fatal("template forged a server-owned snapshot")
	}
	member.Config.Annotations = map[string]string{instancetype.NodeSelectorSnapshotAnnotation: `{"example.com/model":"forged"}`}
	if _, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: member}); err == nil {
		t.Fatal("Config forged a server snapshot")
	}
}

func TestRenderCPUAndDevicesUseSameTemplateWithoutMutatingIt(t *testing.T) {
	env, tmpl := newTestEnv(), newTestTemplate()
	before := tmpl.DeepCopy()
	cpu := agentsv1alpha1.EnvClusterMember{Name: "cpu"}
	gpu := agentsv1alpha1.EnvClusterMember{Name: "gpu", Metadata: agentsv1alpha1.MemberMetadata{Annotations: map[string]string{instancetype.NodeSelectorSnapshotAnnotation: `{"example.com/model":"model-a"}`}}}
	c, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: cpu})
	if err != nil {
		t.Fatal(err)
	}
	g, err := poolrender.RenderSandboxPool(poolrender.Inputs{Env: env, Template: tmpl, Member: gpu})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spec.Template.Spec.NodeSelector) != 0 || g.Spec.Template.Spec.NodeSelector["example.com/model"] != "model-a" {
		t.Fatal("CPU/GPU placement leaked across members")
	}
	if !reflect.DeepEqual(tmpl, before) {
		t.Fatal("shared template mutated")
	}
}
