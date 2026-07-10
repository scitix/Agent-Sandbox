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

package egress

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

func sandboxPod() *corev1.Pod {
	return &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "sandbox", Image: "sbx:1"}},
	}}
}

func poolWithPolicy() *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{Spec: agentsv1alpha1.SandboxPoolSpec{
		NetworkPolicy: &agentsv1alpha1.SandboxNetworkPolicy{
			Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}},
		},
	}}
}

func TestPreCreatePod_NoPolicyIsNoop(t *testing.T) {
	p := New(Config{Image: "idle:1"})
	pod := sandboxPod()
	updated, err := p.PreCreatePod(context.Background(), pod, &agentsv1alpha1.SandboxPool{})
	if err != nil || updated {
		t.Fatalf("no policy => no-op; got updated=%v err=%v", updated, err)
	}
	if len(pod.Spec.InitContainers) != 0 || len(pod.Spec.Volumes) != 0 {
		t.Errorf("no policy must not inject anything: %+v", pod.Spec)
	}
}

func TestPreCreatePod_InjectsSidecarAndInit(t *testing.T) {
	p := New(Config{Image: "idle:1"})
	pod := sandboxPod()
	updated, err := p.PreCreatePod(context.Background(), pod, poolWithPolicy())
	if err != nil || !updated {
		t.Fatalf("policy => inject; got updated=%v err=%v", updated, err)
	}
	if !hasContainer(pod.Spec.InitContainers, initContainerName) {
		t.Error("egress-init init container missing")
	}
	if !hasContainer(pod.Spec.InitContainers, proxyContainerName) {
		t.Error("egress-proxy sidecar missing")
	}
	// Sidecar must be a native sidecar (restartPolicy Always) as uid 1337.
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]
		if c.Name != proxyContainerName {
			continue
		}
		if c.RestartPolicy == nil || *c.RestartPolicy != corev1.ContainerRestartPolicyAlways {
			t.Error("sidecar must be a native sidecar (restartPolicy=Always)")
		}
		if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != egressproxy.DefaultProxyUID {
			t.Error("sidecar must run as proxy uid")
		}
	}
	// Policy volume present, mounted only on the sidecar.
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].Name != policyVolumeName {
		t.Errorf("policy volume missing: %+v", pod.Spec.Volumes)
	}
}

func TestPreCreatePod_StripsNetAdmin(t *testing.T) {
	p := New(Config{Image: "idle:1"})
	pod := sandboxPod()
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN", "SYS_TIME"}},
	}
	if _, err := p.PreCreatePod(context.Background(), pod, poolWithPolicy()); err != nil {
		t.Fatal(err)
	}
	got := pod.Spec.Containers[0].SecurityContext.Capabilities.Add
	for _, c := range got {
		if c == "NET_ADMIN" {
			t.Errorf("NET_ADMIN must be stripped from sandbox container, got %v", got)
		}
	}
	if len(got) != 1 || got[0] != "SYS_TIME" {
		t.Errorf("other caps must be preserved, got %v", got)
	}
}

func TestPreCreatePod_RejectsProxyUIDCollision(t *testing.T) {
	p := New(Config{Image: "idle:1"})
	pod := sandboxPod()
	uid := int64(egressproxy.DefaultProxyUID)
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{RunAsUser: &uid}
	_, err := p.PreCreatePod(context.Background(), pod, poolWithPolicy())
	if err == nil {
		t.Fatal("expected rejection when sandbox container uses the proxy uid")
	}
}

func TestPreCreatePod_MissingImageFailsClosed(t *testing.T) {
	// No flag image AND no pool idle image => fail-closed.
	p := New(Config{Image: ""})
	_, err := p.PreCreatePod(context.Background(), sandboxPod(), poolWithPolicy())
	if err == nil {
		t.Fatal("policy with no image (flag or idleImage) must fail pod creation (fail-closed)")
	}
}

func TestPreCreatePod_FallsBackToIdleImage(t *testing.T) {
	// No flag image, but the pool's idle image (which bundles egress-proxy) is used.
	p := New(Config{Image: ""})
	pool := poolWithPolicy()
	pool.Spec.IdleImage = "reg/agent-sandbox-idle:egress"
	pod := sandboxPod()
	updated, err := p.PreCreatePod(context.Background(), pod, pool)
	if err != nil || !updated {
		t.Fatalf("should inject using idleImage fallback; got updated=%v err=%v", updated, err)
	}
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Image != "reg/agent-sandbox-idle:egress" {
			t.Errorf("injected container %q should use idleImage, got %q",
				pod.Spec.InitContainers[i].Name, pod.Spec.InitContainers[i].Image)
		}
	}
}

func TestPreCreatePod_FlagImageOverridesIdle(t *testing.T) {
	p := New(Config{Image: "flag/img:1"})
	pool := poolWithPolicy()
	pool.Spec.IdleImage = "idle/img:1"
	pod := sandboxPod()
	if _, err := p.PreCreatePod(context.Background(), pod, pool); err != nil {
		t.Fatal(err)
	}
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Image != "flag/img:1" {
			t.Errorf("flag image must take priority over idleImage, got %q", pod.Spec.InitContainers[i].Image)
		}
	}
}
