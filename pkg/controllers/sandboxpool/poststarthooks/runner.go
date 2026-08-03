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

// Package poststarthooks executes post-start hook actions on sandbox pods that have
// just transitioned Starting → Running. It implements sandboxpool.SandboxReadyHook.
package poststarthooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Action describes a single action to execute after a sandbox becomes Running.
// Exactly one of Exec or HTTPPost should be set (mirrors k8s ProbeHandler style).
// The action is serialized as JSON to the SandboxPostStartHooksAnnotationKey pod
// annotation at claim time, then consumed here when the pod reaches Running.
type Action struct {
	// Exec runs a shell command inside the sandbox container.
	Exec *ExecAction `json:"exec,omitempty"`
	// HTTPPost sends a POST request to an in-sandbox HTTP endpoint via the gateway.
	HTTPPost *HTTPPostAction `json:"httpPost,omitempty"`
}

// ExecAction runs a command inside the sandbox container.
type ExecAction struct {
	// Command is passed to sh -c inside the first container.
	Command string `json:"command"`
}

// HTTPPostAction sends a POST to a port/path inside the sandbox, routed through the gateway.
type HTTPPostAction struct {
	// Port is the target port of the in-sandbox service (required).
	Port int32 `json:"port"`
	// Path is the request path, e.g. "/init".
	Path string `json:"path"`
	// Body is an arbitrary JSON object serialized as the request body.
	Body map[string]any `json:"body,omitempty"`
	// Headers are extra HTTP headers to include (e.g. for authentication).
	Headers map[string]string `json:"headers,omitempty"`
}

// defaultBackoff retries a hook roughly 5 times over ~20 seconds.
//
//	Delays: 200ms → 400ms → 800ms → 1.6s → 3.2s (capped at 10s)
var defaultBackoff = retry.DefaultRetry

func init() {
	defaultBackoff.Duration = 200 * time.Millisecond
	defaultBackoff.Factor = 2.0
	defaultBackoff.Steps = 5
	defaultBackoff.Cap = 10 * time.Second
}

// Runner implements sandboxpool.SandboxReadyHook.
// It reads post-start hook actions from pod annotations and executes them
// after the sandbox transitions Starting → Running.
type Runner struct {
	gatewayBaseURL string
	httpClient     *http.Client
	clientset      kubernetes.Interface
	restConfig     *rest.Config
}

// NewRunner creates a Runner. gatewayBaseURL may be empty (HTTP hooks are skipped).
// clientset / restConfig may be nil (Exec hooks are skipped).
func NewRunner(gatewayBaseURL string, clientset kubernetes.Interface, restConfig *rest.Config) *Runner {
	return &Runner{
		gatewayBaseURL: gatewayBaseURL,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		clientset:      clientset,
		restConfig:     restConfig,
	}
}

// egressProxyContainerName is the filter sidecar the egress plugin injects.
const egressProxyContainerName = agentsv1alpha1.EgressProxyContainerName

// OnSandboxReady implements sandboxpool.SandboxReadyHook.
// It is called in a goroutine; errors are logged but never propagated.
//
// The step order is a security contract, not a convenience:
//
//  1. resolve credentials and mint the sandbox's CA (nothing observable yet);
//  2. run the post-start hooks, which install that CA in the sandbox's trust
//     store — merged into the same /init call as the user's env vars;
//  3. push the egress policy;
//  4. arm the sidecar with the credentials.
//
// Arming before the CA is installed would make the sandbox's first intercepted
// request fail its TLS handshake. Both halves of a partial failure are safe:
// without step 4 requests leave carrying only a decoy, and without step 2 they
// do not leave at all. Neither leaks a credential.
func (r *Runner) OnSandboxReady(ctx context.Context, pod *corev1.Pod) {
	plan, err := r.prepareInjection(ctx, pod)
	if err != nil {
		// No credential is armed, so the sandbox runs with decoys only and the
		// upstream rejects it. Loud, but not a leak.
		klog.ErrorS(err, "egress inject: could not prepare credential injection; sandbox will run without it",
			"pod", klog.KObj(pod))
	}
	r.runPostStartHooks(ctx, pod, plan)
	r.pushEgressPolicy(ctx, pod)
	r.pushEgressSecrets(ctx, pod, plan)
}

// OnSandboxRelease implements sandboxpool.SandboxReleaseHook. When the pod
// returns to the pool (Stopping → Idle), it resets the filter sidecar to
// fail-closed so a reused pod never carries the previous sandbox's egress rules
// into the window before the next claim's policy push lands.
func (r *Runner) OnSandboxRelease(ctx context.Context, pod *corev1.Pod) {
	if !hasInitContainer(pod, egressProxyContainerName) {
		return
	}
	err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.execInContainer(ctx, pod, egressProxyContainerName, []string{"/egress-proxy", "reset"}, nil)
	})
	if err != nil {
		klog.ErrorS(err, "egress: failed to reset policy on release", "pod", klog.KObj(pod))
	}
}

// pushEgressPolicy pushes the effective egress policy (from the egress-policy
// annotation) into the filter sidecar via exec. No-op when the pod has no
// policy annotation or no sidecar.
func (r *Runner) pushEgressPolicy(ctx context.Context, pod *corev1.Pod) {
	raw := pod.Annotations[agentsv1alpha1.SandboxEgressPolicyAnnotationKey]
	if raw == "" {
		return
	}
	if !hasInitContainer(pod, egressProxyContainerName) {
		// No sidecar means no iptables redirect either, so nothing filters this
		// pod's traffic — it is UNFILTERED, not fail-closed. The dispatcher
		// refuses such pods for policy-bearing claims (RequireEgressSidecar), so
		// reaching here means something bypassed it; treat as an incident.
		klog.ErrorS(nil, "egress: policy annotation present but no egress-proxy sidecar; this pod's egress is UNFILTERED",
			"pod", klog.KObj(pod))
		return
	}
	err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.execInContainer(ctx, pod, egressProxyContainerName, []string{"/egress-proxy", "set-policy"}, strings.NewReader(raw))
	})
	if err != nil {
		klog.ErrorS(err, "egress: failed to push policy to sidecar; pod egress stays fail-closed", "pod", klog.KObj(pod))
	}
}

// pushEgressSecrets arms the filter sidecar with the resolved credentials, the
// sandbox's CA private key, and the decoy map. This is the only path credential
// material takes into the Pod, and it terminates in a tmpfs the sandbox
// containers do not mount.
//
// It must run after the CA has been installed in the sandbox (see
// OnSandboxReady). On failure the sidecar simply stays unarmed.
func (r *Runner) pushEgressSecrets(ctx context.Context, pod *corev1.Pod, plan *injectionPlan) {
	if plan == nil {
		return
	}
	if !hasInitContainer(pod, egressProxyContainerName) {
		klog.ErrorS(nil, "egress inject: injection configured but no egress-proxy sidecar; nothing will be injected",
			"pod", klog.KObj(pod))
		return
	}
	payload, err := json.Marshal(plan.secrets)
	if err != nil {
		klog.ErrorS(err, "egress inject: failed to encode secrets payload", "pod", klog.KObj(pod))
		return
	}
	err = retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.execInContainer(ctx, pod, egressProxyContainerName,
			[]string{"/egress-proxy", "set-secrets"}, bytes.NewReader(payload))
	})
	if err != nil {
		// Deliberately does not log the payload.
		klog.ErrorS(err, "egress inject: failed to push credentials to sidecar; requests will carry only decoys",
			"pod", klog.KObj(pod), "rules", len(plan.secrets.Rules))
		return
	}
	klog.V(2).InfoS("egress inject: credentials armed", "pod", klog.KObj(pod),
		"rules", len(plan.secrets.Rules), "substitutions", len(plan.secrets.Substitutions))
}

// runPostStartHooks executes the user-declared post-start hooks recorded on the
// pod's annotation, after folding in the CA certificate and decoy env vars that
// credential injection needs the sandbox to have.
func (r *Runner) runPostStartHooks(ctx context.Context, pod *corev1.Pod, plan *injectionPlan) {
	var hooks []Action
	if raw, ok := pod.Annotations[agentsv1alpha1.SandboxPostStartHooksAnnotationKey]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &hooks); err != nil {
			klog.ErrorS(err, "poststarthooks: failed to decode hooks annotation",
				"pod", klog.KObj(pod))
			return
		}
	}
	if plan != nil {
		hooks = mergeInitHook(hooks, plan.caCertPEM, plan.envVars)
	}
	if len(hooks) == 0 {
		return
	}

	sandboxID := pod.Annotations[agentsv1alpha1.SandboxIDAnnotationKey]
	if sandboxID == "" {
		klog.ErrorS(nil, "poststarthooks: pod has no sandbox-id annotation, skipping",
			"pod", klog.KObj(pod))
		return
	}

	for i, hook := range hooks {
		hookIdx := i
		h := hook
		err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
			return r.executeHook(ctx, pod, sandboxID, h)
		})
		if err != nil {
			klog.ErrorS(err, "poststarthooks: hook failed after retries",
				"pod", klog.KObj(pod), "sandboxID", sandboxID, "hookIndex", hookIdx)
		}
	}
}

func (r *Runner) executeHook(ctx context.Context, pod *corev1.Pod, sandboxID string, hook Action) error {
	switch {
	case hook.Exec != nil:
		return r.execHook(ctx, pod, hook.Exec)
	case hook.HTTPPost != nil:
		return r.httpPostHook(ctx, sandboxID, hook.HTTPPost)
	default:
		return fmt.Errorf("empty hook action (neither exec nor httpPost is set)")
	}
}

// execHook runs a shell command inside the sandbox's first container.
func (r *Runner) execHook(ctx context.Context, pod *corev1.Pod, action *ExecAction) error {
	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("exec hook skipped: pod has no containers")
	}
	return r.execInContainer(ctx, pod, pod.Spec.Containers[0].Name, []string{"sh", "-c", action.Command}, nil)
}

// execInContainer runs command in the named container, optionally piping stdin.
// Used both for user post-start hooks (sandbox container) and control-plane
// egress policy push/reset (egress-proxy sidecar).
func (r *Runner) execInContainer(ctx context.Context, pod *corev1.Pod, container string, command []string, stdin io.Reader) error {
	if r.clientset == nil || r.restConfig == nil {
		return fmt.Errorf("exec skipped: kubernetes clientset or rest config not configured")
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	execReq := r.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(r.restConfig, http.MethodPost, execReq.URL())
	if err != nil {
		return fmt.Errorf("exec: failed to create executor: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	if err := executor.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
		Tty:    false,
	}); err != nil {
		return fmt.Errorf("exec: command %v in %s failed: %w (stderr: %s)", command, container, err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

func hasInitContainer(pod *corev1.Pod, name string) bool {
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == name {
			return true
		}
	}
	return false
}

// httpPostHook sends a POST request through the gateway to an in-sandbox HTTP endpoint.
func (r *Runner) httpPostHook(ctx context.Context, sandboxID string, action *HTTPPostAction) error {
	if r.gatewayBaseURL == "" {
		return fmt.Errorf("http-post hook skipped: gateway base URL not configured")
	}

	url := fmt.Sprintf("%s/sandboxes/%s/%d%s",
		strings.TrimRight(r.gatewayBaseURL, "/"), sandboxID, action.Port, action.Path)

	var bodyReader io.Reader
	if len(action.Body) > 0 {
		data, err := json.Marshal(action.Body)
		if err != nil {
			return fmt.Errorf("http-post hook: failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return fmt.Errorf("http-post hook: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range action.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http-post hook: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http-post hook: endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
