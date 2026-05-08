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

package plugins

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

const testLabelRan = "true"

// mutatingPlugin injects a label into the pod to prove it ran.
type mutatingPlugin struct {
	BasePlugin
	label string
}

func (p *mutatingPlugin) Name() string { return "mutating-" + p.label }

func (p *mutatingPlugin) PreCreatePod(_ context.Context, pod *corev1.Pod, _ *agentsv1alpha1.SandboxPool) *domain.AppError {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels["test/ran-"+p.label] = testLabelRan
	return nil
}

// failingPlugin always returns an error from PreCreatePod.
type failingPlugin struct {
	BasePlugin
}

func (p *failingPlugin) Name() string { return "failing" }

func (p *failingPlugin) PreCreatePod(_ context.Context, _ *corev1.Pod, _ *agentsv1alpha1.SandboxPool) *domain.AppError {
	return domain.NewInternal("intentional test failure", nil)
}

func TestPreCreatePodHooks_NilManager(t *testing.T) {
	var m *PluginManager
	pod := &corev1.Pod{}
	pool := &agentsv1alpha1.SandboxPool{}
	if err := m.PreCreatePodHooks(context.Background(), pod, pool); err != nil {
		t.Fatalf("nil manager should be no-op, got: %v", err)
	}
}

func TestPreCreatePodHooks_SinglePlugin_MutatesPod(t *testing.T) {
	m := NewPluginManager(&mutatingPlugin{label: "a"})
	pod := &corev1.Pod{}
	pool := &agentsv1alpha1.SandboxPool{}

	if err := m.PreCreatePodHooks(context.Background(), pod, pool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.Labels["test/ran-a"] != testLabelRan {
		t.Fatal("plugin did not mutate pod")
	}
}

func TestPreCreatePodHooks_MultiPlugin_ShortCircuitOnError(t *testing.T) {
	called := false
	afterPlugin := &mutatingPlugin{label: "after"}
	// override PreCreatePod to track call
	_ = afterPlugin

	m := NewPluginManager(
		&mutatingPlugin{label: "before"},
		&failingPlugin{},
		&mutatingPlugin{label: "after"},
	)
	pod := &corev1.Pod{}
	pool := &agentsv1alpha1.SandboxPool{}

	err := m.PreCreatePodHooks(context.Background(), pod, pool)
	if err == nil {
		t.Fatal("expected error from failingPlugin")
	}
	// "before" ran, "after" must NOT have run
	if pod.Labels["test/ran-before"] != testLabelRan {
		t.Fatal("first plugin should have run before the error")
	}
	if pod.Labels["test/ran-after"] == testLabelRan {
		t.Fatal("third plugin must not run after error (short-circuit)")
	}
	_ = called
}
