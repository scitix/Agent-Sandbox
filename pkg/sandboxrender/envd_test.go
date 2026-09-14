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

package sandboxrender

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func embWithContainer(env ...corev1.EnvVar) *agentsv1alpha1.EmbeddedSandboxTemplate {
	return &agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sandbox", Image: "busybox", Env: env}},
			},
		},
	}
}

func envValue(c corev1.Container, name string) (string, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// Empty() short-circuits Apply entirely, so a field missing from it means an
// Env whose only override is that field renders to nothing and the setting
// silently never takes effect.
func TestOptions_EmptyAccountsForEnvdVerbose(t *testing.T) {
	if !(Options{}).Empty() {
		t.Fatal("a zero Options must be empty")
	}
	for _, v := range []*bool{ptr.To(true), ptr.To(false)} {
		if (Options{EnvdVerbose: v}).Empty() {
			t.Fatalf("EnvdVerbose=%v must make Options observable", *v)
		}
	}
}

func TestApply_EnvdVerbose(t *testing.T) {
	t.Run("nil leaves the template alone", func(t *testing.T) {
		emb := embWithContainer()
		if err := Apply(emb, Options{}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if _, ok := envValue(emb.Template.Spec.Containers[0], AgentEnvVerbose); ok {
			t.Fatal("no opinion must write nothing: an env var here rolls every pool")
		}
	})

	for _, want := range []bool{true, false} {
		t.Run("writes the chosen value", func(t *testing.T) {
			emb := embWithContainer()
			if err := Apply(emb, Options{EnvdVerbose: ptr.To(want)}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			got, ok := envValue(emb.Template.Spec.Containers[0], AgentEnvVerbose)
			if !ok {
				t.Fatalf("%s not set", AgentEnvVerbose)
			}
			if (got == "true") != want {
				t.Fatalf("expected %v, got %q", want, got)
			}
		})
	}

	// Duplicate names in a pod spec are legal and the last wins, so appending
	// would work by accident right up until something reordered the list.
	t.Run("replaces rather than appends", func(t *testing.T) {
		emb := embWithContainer(corev1.EnvVar{Name: AgentEnvVerbose, Value: "true"})
		if err := Apply(emb, Options{EnvdVerbose: ptr.To(false)}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		c := emb.Template.Spec.Containers[0]
		count := 0
		for _, e := range c.Env {
			if e.Name == AgentEnvVerbose {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly one %s entry, got %d", AgentEnvVerbose, count)
		}
		if got, _ := envValue(c, AgentEnvVerbose); got != "false" {
			t.Fatalf("expected the override to win, got %q", got)
		}
	})

	// The setting is only meaningful on the sandbox container, and the renderer
	// must not invent one.
	t.Run("refuses a template with no container", func(t *testing.T) {
		emb := &agentsv1alpha1.EmbeddedSandboxTemplate{}
		if err := Apply(emb, Options{EnvdVerbose: ptr.To(true)}); err == nil {
			t.Fatal("expected an error for a template with no containers")
		}
	})
}

// Unset means enabled, and every reader has to agree — the console renders a
// switch from the same rule.
func TestEnvdSpec_VerboseEnabled(t *testing.T) {
	var nilSpec *agentsv1alpha1.EnvdSpec
	if !nilSpec.VerboseEnabled() {
		t.Error("a nil spec means enabled")
	}
	if !(&agentsv1alpha1.EnvdSpec{}).VerboseEnabled() {
		t.Error("an unset field means enabled")
	}
	if (&agentsv1alpha1.EnvdSpec{Verbose: ptr.To(false)}).VerboseEnabled() {
		t.Error("an explicit false means disabled")
	}
}
