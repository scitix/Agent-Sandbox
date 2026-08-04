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
	"k8s.io/apimachinery/pkg/util/intstr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
)

const (
	pluginName         = "egress-network-policy"
	initContainerName  = "egress-init"
	proxyContainerName = agentsv1alpha1.EgressProxyContainerName
	policyVolumeName   = "egress-policy"
	policyMountDir     = "/var/run/egress"
)

// Config parameterizes the injected containers.
type Config struct {
	// LegacySidecar injects the proxy as an ordinary container instead of a
	// native sidecar, for API servers that do not support
	// initContainers[].restartPolicy — Kubernetes < 1.28, or 1.28 without the
	// SidecarContainers gate. Those API servers *silently prune* the field, so
	// the proxy degrades into a plain init container running a process that
	// never exits and the Pod hangs in Init forever. Detect it with
	// `kubectl explain pod.spec.initContainers.restartPolicy`.
	//
	// The cost is the ordering guarantee: a native sidecar is up before the app
	// containers start, whereas an ordinary container starts alongside them, so
	// early sandbox traffic can hit the redirect before the proxy listens. That
	// window fails closed (connection refused, nothing escapes unfiltered) and
	// is confined to a Pod's own startup — the readiness probe below keeps a Pod
	// out of the claimable set until the proxy answers.
	LegacySidecar bool

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
	if agentsv1alpha1.PodHasEgressProxy(pod) {
		return false, nil // already injected (defensive; createPod builds a fresh Pod)
	}

	// Resolve the sidecar/init image: the operator's --egress-proxy-image flag
	// takes priority; otherwise fall back to the Pool's idle image, which bundles
	// the egress-proxy binary. This lets egress work without any extra operator
	// config. If neither is set, fail pod creation (fail-closed).
	img := p.cfg.Image
	if img == "" {
		img = pool.Spec.IdleImage
	}
	if img == "" {
		return false, plugins.NewInternal("egress plugin: no sidecar image (set --egress-proxy-image or the pool's idleImage)", nil)
	}

	// A sandbox process running as the proxy uid would be exempted from the
	// redirect (owner match) and bypass the filter. Reject explicit collisions.
	if err := rejectProxyUIDCollision(pod); err != nil {
		return false, err
	}

	// The policy volume holds the credential payload and the sandbox's CA
	// private key. A sandbox container that mounted it could simply read them,
	// which would defeat the entire point of brokering credentials outside the
	// sandbox.
	if err := rejectPolicyVolumeMount(pod); err != nil {
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
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, p.initContainer(img))
	if p.cfg.LegacySidecar {
		// Ordinary container: the redirect is already installed by the init
		// container above, so traffic that beats the proxy is refused rather
		// than leaked. Appended last so the sandbox stays containers[0], which
		// the in-place image update patches by index.
		pod.Spec.Containers = append(pod.Spec.Containers, p.legacySidecar(img))
		return true, nil
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, p.sidecar(img))
	return true, nil
}

func (p *Plugin) initContainer(img string) corev1.Container {
	return corev1.Container{
		Name:            initContainerName,
		Image:           img,
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

func (p *Plugin) sidecar(img string) corev1.Container {
	always := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:            proxyContainerName,
		Image:           img,
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

// legacySidecar is the same container without the native-sidecar restartPolicy,
// for API servers that would prune that field (see Config.LegacySidecar).
//
// It carries a readiness probe the native form does not need: as an ordinary
// container it has no start-ordering guarantee, so gating Pod readiness on the
// proxy actually listening is what keeps a half-started Pod out of the claimable
// set (the claim path only ever hands out Ready idle Pods).
func (p *Plugin) legacySidecar(img string) corev1.Container {
	c := p.sidecar(img)
	c.RestartPolicy = nil
	c.ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(egressproxy.DefaultHTTPPort),
			},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       2,
		FailureThreshold:    30,
	}
	return c
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

// rejectPolicyVolumeMount refuses a Pod whose sandbox containers mount the
// sidecar's private volume. The volume carries pushed credentials and the
// per-sandbox CA key; anything that can read it can read the secrets the
// sandbox is specifically not supposed to see.
func rejectPolicyVolumeMount(pod *corev1.Pod) *domain.AppError {
	check := func(kind string, cs []corev1.Container) *domain.AppError {
		for i := range cs {
			for _, m := range cs[i].VolumeMounts {
				if m.Name == policyVolumeName {
					return plugins.NewInvalidSpec(fmt.Sprintf(
						"egress network policy is enabled but %s %q mounts the %q volume, "+
							"which holds the filter's credential payload and CA key; remove the mount",
						kind, cs[i].Name, policyVolumeName), nil)
				}
			}
		}
		return nil
	}
	if err := check("container", pod.Spec.Containers); err != nil {
		return err
	}
	return check("init container", pod.Spec.InitContainers)
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
