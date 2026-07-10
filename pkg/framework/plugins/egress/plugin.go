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

// Package egress provides the PreCreatePod plugin that injects the sandbox
// egress filter (an init container that installs the iptables redirect and a
// native sidecar that runs the proxy) into Pods of Pools whose Spec.NetworkPolicy
// is set. Enforcement lives entirely in the Pod network namespace + the sidecar,
// so it is independent of the sandbox container's image (survives in-place image
// swap) and of the cluster CNI (works on Calico / Aliyun ENI alike).
package egress

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
)

const (
	pluginName         = "egress-network-policy"
	initContainerName  = "egress-init"
	proxyContainerName = "egress-proxy"
	policyVolumeName   = "egress-policy"
	policyMountDir     = "/var/run/egress"
)

// Config parameterizes the injected containers.
type Config struct {
	// Image is the container image that carries the egress-proxy binary. Both
	// the init container and the sidecar run it (different subcommands). Defaults
	// to the operator's idle image, which is guaranteed present on pool nodes.
	Image string
}

// Plugin injects the egress filter sidecar. It self-gates on
// pool.Spec.NetworkPolicy — Pools without a policy are untouched.
type Plugin struct {
	plugins.BasePlugin
	cfg Config
}

// New returns an egress injection Plugin.
func New(cfg Config) *Plugin { return &Plugin{cfg: cfg} }

func (p *Plugin) Name() string { return pluginName }

// PreCreatePod injects the init container + native sidecar + policy volume when
// the owning Pool has a NetworkPolicy, and hardens the sandbox containers
// (strip NET_ADMIN, reject a uid collision with the proxy). Idempotent.
func (p *Plugin) PreCreatePod(_ context.Context, pod *corev1.Pod, pool *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	if pool == nil || pool.Spec.NetworkPolicy == nil {
		return false, nil
	}
	if hasContainer(pod.Spec.InitContainers, proxyContainerName) {
		return false, nil // already injected (defensive; createPod builds a fresh Pod)
	}
	if p.cfg.Image == "" {
		return false, plugins.NewInternal("egress plugin: no proxy image configured", nil)
	}

	// A sandbox process running as the proxy uid would be exempted from the
	// redirect (owner match) and bypass the filter. Reject explicit collisions.
	if err := rejectProxyUIDCollision(pod); err != nil {
		return false, err
	}

	// Harden app containers: they must not be able to reprogram the firewall.
	for i := range pod.Spec.Containers {
		stripNetAdmin(&pod.Spec.Containers[i])
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name:         policyVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}},
	})

	// Append at the end of InitContainers so any existing injector init
	// containers (tini/envd) still run with unfiltered network; the redirect is
	// installed last, immediately before the native sidecar comes up and the app
	// containers start.
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, p.initContainer(), p.sidecar())
	return true, nil
}

func (p *Plugin) initContainer() corev1.Container {
	return corev1.Container{
		Name:            initContainerName,
		Image:           p.cfg.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/egress-proxy", "install-redirect"},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr(int64(0)),
			AllowPrivilegeEscalation: ptr(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add:  []corev1.Capability{"NET_ADMIN"},
			},
		},
		Resources: minimalResources(),
	}
}

func (p *Plugin) sidecar() corev1.Container {
	always := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:            proxyContainerName,
		Image:           p.cfg.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/egress-proxy", "serve"},
		RestartPolicy:   &always, // native sidecar (k8s >= 1.29)
		VolumeMounts: []corev1.VolumeMount{
			{Name: policyVolumeName, MountPath: policyMountDir},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr(int64(egressproxy.DefaultProxyUID)),
			RunAsNonRoot:             ptr(true),
			AllowPrivilegeEscalation: ptr(false),
			ReadOnlyRootFilesystem:   ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		Resources: minimalResources(),
	}
}

func rejectProxyUIDCollision(pod *corev1.Pod) *domain.AppError {
	const uid = int64(egressproxy.DefaultProxyUID)
	if sc := pod.Spec.SecurityContext; sc != nil && sc.RunAsUser != nil && *sc.RunAsUser == uid {
		return proxyUIDErr("pod securityContext")
	}
	for i := range pod.Spec.Containers {
		if sc := pod.Spec.Containers[i].SecurityContext; sc != nil && sc.RunAsUser != nil && *sc.RunAsUser == uid {
			return proxyUIDErr("container " + pod.Spec.Containers[i].Name)
		}
	}
	return nil
}

func proxyUIDErr(where string) *domain.AppError {
	return plugins.NewInvalidSpec(
		fmt.Sprintf("egress network policy is enabled but %s runs as uid %d, which the egress proxy reserves; use a different uid",
			where, egressproxy.DefaultProxyUID), nil)
}

// stripNetAdmin removes CAP_NET_ADMIN from a container so the (untrusted)
// sandbox process cannot flush or rewrite the redirect rules.
func stripNetAdmin(c *corev1.Container) {
	if c.SecurityContext == nil || c.SecurityContext.Capabilities == nil {
		return
	}
	caps := c.SecurityContext.Capabilities
	filtered := caps.Add[:0]
	for _, cap := range caps.Add {
		if cap != "NET_ADMIN" && cap != "ALL" {
			filtered = append(filtered, cap)
		}
	}
	caps.Add = filtered
}

func hasContainer(cs []corev1.Container, name string) bool {
	for i := range cs {
		if cs[i].Name == name {
			return true
		}
	}
	return false
}

func minimalResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func ptr[T any](v T) *T { return &v }
