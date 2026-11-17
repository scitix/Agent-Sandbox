// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0
package sandboxrender_test

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

func TestApplyInstanceSelectorPreservesAffinityAndTemplateSelector(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "1", "4Gi")
	tmpl.Template.Spec.NodeSelector = map[string]string{"example.com/zone": "zone-a"}
	affinity := &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "example.com/instance", Operator: corev1.NodeSelectorOpIn, Values: []string{"shape-a"}}}}}}}}
	tmpl.Template.Spec.Affinity = affinity.DeepCopy()
	selector := map[string]string{"example.com/model": "model-a"}
	if err := sandboxrender.Apply(&tmpl, sandboxrender.Options{NodeSelector: selector}); err != nil {
		t.Fatal(err)
	}
	if tmpl.Template.Spec.NodeSelector["example.com/zone"] != "zone-a" || tmpl.Template.Spec.NodeSelector["example.com/model"] != "model-a" {
		t.Fatal("selectors were not AND-merged")
	}
	if !reflect.DeepEqual(tmpl.Template.Spec.Affinity, affinity) {
		t.Fatal("existing affinity changed")
	}
	tmpl.Template.Spec.NodeSelector["example.com/model"] = "mutated"
	if selector["example.com/model"] != "model-a" {
		t.Fatal("caller selector mutated")
	}
}

func TestApplyInstanceSelectorConflictFailsBeforeMutation(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "1", "4Gi")
	tmpl.Template.Spec.NodeSelector = map[string]string{"example.com/model": "model-a"}
	before := tmpl.DeepCopy()
	if err := sandboxrender.Apply(&tmpl, sandboxrender.Options{Image: "changed:v1", NodeSelector: map[string]string{"example.com/model": "model-b"}}); err == nil {
		t.Fatal("conflicting selectors accepted")
	}
	if !reflect.DeepEqual(&tmpl, before) {
		t.Fatal("conflict mutated template")
	}
}
