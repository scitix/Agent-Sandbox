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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
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
	// crClient reads the owning SandboxPool (for runtime readiness probes) and
	// writes the arming annotations back onto the Pod. Nil disables the
	// readiness wait; arming then still runs, it just cannot pre-check the
	// runtimes.
	crClient client.Client
}

// NewRunner creates a Runner. gatewayBaseURL may be empty (it is only a
// fallback for post-start HTTP hooks; the normal path dials the Pod directly).
// clientset / restConfig may be nil (Exec hooks are skipped); crClient may be
// nil (runtime readiness pre-check and arming annotations are skipped).
func NewRunner(gatewayBaseURL string, clientset kubernetes.Interface, restConfig *rest.Config, crClient client.Client) *Runner {
	return &Runner{
		gatewayBaseURL: gatewayBaseURL,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		clientset:      clientset,
		restConfig:     restConfig,
		crClient:       crClient,
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
	sandboxID := pod.Annotations[agentsv1alpha1.SandboxIDAnnotationKey]
	start := time.Now()

	if err := r.arm(ctx, pod); err != nil {
		klog.ErrorS(err, "arming failed; sandbox will not be delivered",
			"pod", klog.KObj(pod), "sandboxID", sandboxID)
		r.markArmResult(ctx, pod, "", err)
		pkgmetrics.SandboxArmTotal.WithLabelValues(pod.Namespace, poolOf(pod), armOutcome(err)).Inc()
		return
	}

	r.markArmResult(ctx, pod, sandboxID, nil)
	pkgmetrics.SandboxArmTotal.WithLabelValues(pod.Namespace, poolOf(pod), armOutcomeSuccess).Inc()
	pkgmetrics.SandboxArmDuration.WithLabelValues(pod.Namespace, poolOf(pod)).Observe(time.Since(start).Seconds())
}

// arm runs every step that has to succeed before a sandbox may be handed to its
// caller, and returns the first failure. The step order is a security contract,
// not a convenience (see the doc comment on OnSandboxReady).
//
// Step 0 exists because the phase transition that triggers this hook only
// compares container image digests — it does not wait for the runtime inside
// the new container to answer. Without it, "armed" would mean "the image was
// swapped", which is not the same as "the sandbox is usable".
func (r *Runner) arm(ctx context.Context, pod *corev1.Pod) error {
	deadline := armDeadline(pod)
	armCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	if err := r.waitRuntimesReady(armCtx, pod); err != nil {
		return err
	}

	plan, err := r.prepareInjection(armCtx, pod)
	if err != nil {
		// Refusing here is deliberate: continuing would hand back a sandbox that
		// looks healthy but reaches every upstream with a decoy, which surfaces
		// as an opaque 401 inside the user's own code.
		return fmt.Errorf("prepare credential injection: %w", err)
	}
	if err := r.runPostStartHooks(armCtx, pod, plan); err != nil {
		return err
	}
	if err := r.pushEgressPolicy(armCtx, pod); err != nil {
		return err
	}
	return r.pushEgressSecrets(armCtx, pod, plan)
}

// OnSandboxRelease implements sandboxpool.SandboxReleaseHook. When the pod
// returns to the pool (Stopping → Idle), it resets the filter sidecar to
// fail-closed so a reused pod never carries the previous sandbox's egress rules
// into the window before the next claim's policy push lands.
func (r *Runner) OnSandboxRelease(ctx context.Context, pod *corev1.Pod) {
	if !agentsv1alpha1.PodHasEgressProxy(pod) {
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
func (r *Runner) pushEgressPolicy(ctx context.Context, pod *corev1.Pod) error {
	raw := pod.Annotations[agentsv1alpha1.SandboxEgressPolicyAnnotationKey]
	if raw == "" {
		return nil
	}
	if !agentsv1alpha1.PodHasEgressProxy(pod) {
		// No sidecar means no iptables redirect either, so nothing filters this
		// pod's traffic — it is UNFILTERED, not fail-closed. The dispatcher
		// refuses such pods for policy-bearing claims (RequireEgressSidecar), so
		// reaching here means something bypassed it; treat as an incident.
		klog.ErrorS(nil, "egress: policy annotation present but no egress-proxy sidecar; this pod's egress is UNFILTERED",
			"pod", klog.KObj(pod))
		return fmt.Errorf("egress policy requested but pod has no egress-proxy sidecar")
	}
	err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.execInContainer(ctx, pod, egressProxyContainerName, []string{"/egress-proxy", "set-policy"}, strings.NewReader(raw))
	})
	if err != nil {
		return fmt.Errorf("push egress policy to sidecar: %w", err)
	}
	return nil
}

// pushEgressSecrets arms the filter sidecar with the resolved credentials, the
// sandbox's CA private key, and the decoy map. This is the only path credential
// material takes into the Pod, and it terminates in a tmpfs the sandbox
// containers do not mount.
//
// It must run after the CA has been installed in the sandbox (see
// OnSandboxReady). On failure the sidecar simply stays unarmed.
func (r *Runner) pushEgressSecrets(ctx context.Context, pod *corev1.Pod, plan *injectionPlan) error {
	if plan == nil {
		return nil
	}
	if !agentsv1alpha1.PodHasEgressProxy(pod) {
		klog.ErrorS(nil, "egress inject: injection configured but no egress-proxy sidecar; nothing will be injected",
			"pod", klog.KObj(pod))
		return fmt.Errorf("credential injection configured but pod has no egress-proxy sidecar")
	}
	payload, err := json.Marshal(plan.secrets)
	if err != nil {
		return fmt.Errorf("encode secrets payload: %w", err)
	}
	err = retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.execInContainer(ctx, pod, egressProxyContainerName,
			[]string{"/egress-proxy", "set-secrets"}, bytes.NewReader(payload))
	})
	if err != nil {
		// Deliberately does not name the payload in the error.
		return fmt.Errorf("push credentials to sidecar (%d rules): %w", len(plan.secrets.Rules), err)
	}
	klog.V(2).InfoS("egress inject: credentials armed", "pod", klog.KObj(pod),
		"rules", len(plan.secrets.Rules), "substitutions", len(plan.secrets.Substitutions))
	return nil
}

// runPostStartHooks executes the user-declared post-start hooks recorded on the
// pod's annotation, after folding in the CA certificate and decoy env vars that
// credential injection needs the sandbox to have.
func (r *Runner) runPostStartHooks(ctx context.Context, pod *corev1.Pod, plan *injectionPlan) error {
	var hooks []Action
	if raw, ok := pod.Annotations[agentsv1alpha1.SandboxPostStartHooksAnnotationKey]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &hooks); err != nil {
			return fmt.Errorf("decode post-start hooks annotation: %w", err)
		}
	}
	// mergeInitHook is called even without an injection plan so the /init call
	// always happens: envd may be gated on having received one, and a sandbox
	// with nothing to deliver would otherwise never lift that gate.
	if plan != nil {
		hooks = mergeInitHook(hooks, plan.caCertPEM, plan.envVars)
	} else {
		hooks = mergeInitHook(hooks, "", nil)
	}
	if len(hooks) == 0 {
		return nil
	}

	sandboxID := pod.Annotations[agentsv1alpha1.SandboxIDAnnotationKey]
	if sandboxID == "" {
		return fmt.Errorf("pod has no sandbox-id annotation")
	}

	for i, hook := range hooks {
		hookIdx := i
		h := hook
		err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
			return r.executeHook(ctx, pod, h)
		})
		if err != nil {
			return fmt.Errorf("post-start hook %d: %w", hookIdx, err)
		}
	}
	return nil
}

func (r *Runner) executeHook(ctx context.Context, pod *corev1.Pod, hook Action) error {
	switch {
	case hook.Exec != nil:
		return r.execHook(ctx, pod, hook.Exec)
	case hook.HTTPPost != nil:
		return r.httpPostHook(ctx, pod, hook.HTTPPost)
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

// httpPostHook sends a POST request to an in-sandbox HTTP endpoint.
//
// It dials the Pod directly rather than going through the data-plane gateway.
// Two reasons, both load-bearing: the gateway refuses to route to a sandbox
// that is not armed yet, and arming is exactly what this call is part of — via
// the gateway the hook would be waiting on itself. Dialling the Pod also drops
// the dependency on the ExtProc route having propagated. The gateway remains a
// fallback for the case where the Pod has no IP yet.
func (r *Runner) httpPostHook(ctx context.Context, pod *corev1.Pod, action *HTTPPostAction) error {
	var url string
	switch {
	case pod.Status.PodIP != "":
		// JoinHostPort brackets an IPv6 literal; a bare "%s:%d" would not.
		url = "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(int(action.Port))) + action.Path
	case r.gatewayBaseURL != "":
		sandboxID := pod.Annotations[agentsv1alpha1.SandboxIDAnnotationKey]
		url = fmt.Sprintf("%s/sandboxes/%s/%d%s",
			strings.TrimRight(r.gatewayBaseURL, "/"), sandboxID, action.Port, action.Path)
	default:
		return fmt.Errorf("http-post hook skipped: pod has no IP and no gateway base URL is configured")
	}

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

// ---------------------------------------------------------------------------
// Arming: readiness pre-check, deadline, and the annotation that records it
// ---------------------------------------------------------------------------

const (
	// minArmBudget is the floor for how long arming may take, used when the
	// pod carries no startup-timeout or has already burned through it. Arming
	// is several round trips (probe, /init, two execs) and a claim that raced
	// close to its deadline should still get a fair chance rather than fail on
	// arithmetic.
	minArmBudget = 30 * time.Second
	// maxArmBudget caps the wait so a permanently sick runtime cannot pin a
	// goroutine (and a caller) for the whole startup timeout of a slow pool.
	maxArmBudget = 10 * time.Minute
	// readyPollInterval is how often the runtime readiness probes are retried.
	readyPollInterval = 250 * time.Millisecond
)

// armDeadline derives when arming must give up: the sandbox's own startup
// timeout, measured from when it was claimed, clamped into [min, max].
func armDeadline(pod *corev1.Pod) time.Time {
	now := time.Now()
	budget := maxArmBudget

	if raw := pod.Annotations[agentsv1alpha1.SandboxStartupTimeoutAnnotationKey]; raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			budget = time.Duration(secs) * time.Second
			if claimedRaw := pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey]; claimedRaw != "" {
				if claimedAt, perr := time.Parse(time.RFC3339, claimedRaw); perr == nil {
					budget -= now.Sub(claimedAt)
				}
			}
		}
	}

	budget = min(max(budget, minArmBudget), maxArmBudget)
	return now.Add(budget)
}

// waitRuntimesReady blocks until every runtime declared by the owning pool
// answers its readiness probe, or ctx expires.
//
// This is the step that gives "armed" its meaning. The phase transition that
// triggered this hook only compared image digests (see
// inplaceupdate.IsInplaceUpdateCompleted) — the container is running the right
// image, but nothing has checked that the runtime inside it is listening.
//
// Probes are sent straight to the Pod IP: the gateway is not routable for an
// unarmed sandbox, and going through it would also make readiness depend on
// ExtProc route propagation. Only HTTPGet probes are checked; a runtime with no
// probe, or with a TCPSocket/Exec probe, counts as ready (same rule the
// readiness API applies).
func (r *Runner) waitRuntimesReady(ctx context.Context, pod *corev1.Pod) error {
	targets, err := r.readinessTargets(ctx, pod)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		pending := targets[:0:0]
		for _, t := range targets {
			if err := probeOnce(ctx, r.httpClient, t.url); err != nil {
				lastErr = fmt.Errorf("runtime %q not ready at %s: %w", t.runtime, t.url, err)
				pending = append(pending, t)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		targets = pending

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for sandbox runtimes to become ready: %w", lastErr)
		case <-ticker.C:
		}
	}
}

type readinessTarget struct {
	runtime string
	url     string
}

// readinessTargets builds the probe URL for each runtime of the pod's pool that
// declares an HTTPGet readiness probe.
func (r *Runner) readinessTargets(ctx context.Context, pod *corev1.Pod) ([]readinessTarget, error) {
	if r.crClient == nil {
		return nil, nil
	}
	poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	if poolName == "" {
		return nil, nil
	}
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("pod has no IP; cannot probe runtime readiness")
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if err := r.crClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: poolName}, pool); err != nil {
		return nil, fmt.Errorf("load pool %s/%s for readiness probes: %w", pod.Namespace, poolName, err)
	}

	targets := make([]readinessTarget, 0, len(pool.Spec.Runtimes))
	for _, rt := range pool.Spec.Runtimes {
		probe := rt.ReadinessProbe
		if probe == nil || probe.HTTPGet == nil {
			continue
		}
		port := probe.HTTPGet.Port.IntValue()
		if port == 0 && rt.Port != nil {
			port = int(*rt.Port)
		}
		if port == 0 {
			continue
		}
		path := probe.HTTPGet.Path
		if path == "" {
			path = "/"
		} else if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		targets = append(targets, readinessTarget{
			runtime: rt.Name,
			url:     "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(port)) + path,
		})
	}
	return targets, nil
}

// probeOnce issues one GET and reports whether the endpoint answered with a
// non-error status. Matches kubelet's rule: 2xx and 3xx are ready.
func probeOnce(ctx context.Context, hc *http.Client, url string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("probe returned %d", resp.StatusCode)
	}
	return nil
}

// markArmResult records the outcome of arming on the Pod. Exactly one of the
// two annotations is ever present: success carries the sandbox ID (so a
// recycled Pod's stale mark cannot be read as an arming of the current claim),
// failure carries the reason. Both are managed annotation keys, so releasing
// the sandbox strips them and the next claim starts unarmed.
func (r *Runner) markArmResult(ctx context.Context, pod *corev1.Pod, sandboxID string, armErr error) {
	if r.crClient == nil {
		return
	}

	key, value := agentsv1alpha1.SandboxArmedAnnotationKey, sandboxID
	if armErr != nil {
		key, value = agentsv1alpha1.SandboxArmErrorAnnotationKey, truncateReason(armErr.Error())
	}

	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{%q:%q}}}`, key, value)
	err := retry.OnError(defaultBackoff, func(error) bool { return true }, func() error {
		return r.crClient.Patch(ctx, pod.DeepCopy(), client.RawPatch(types.MergePatchType, patch))
	})
	if err != nil {
		// The sandbox is (or is not) armed regardless; only the record failed.
		// The create path will time out waiting and release the pod, which is
		// the safe direction.
		klog.ErrorS(err, "arming: failed to record result on pod",
			"pod", klog.KObj(pod), "annotation", key)
	}
}

// truncateReason bounds an arming failure so it cannot blow the 256 KiB
// annotation budget on a pathological error chain.
func truncateReason(s string) string {
	const maxReason = 512
	if len(s) <= maxReason {
		return s
	}
	return s[:maxReason] + "…"
}

// poolOf returns the pod's pool name for metric labels.
func poolOf(pod *corev1.Pod) string {
	return pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
}

// Arming outcomes, as reported on the SandboxArmTotal metric.
const (
	armOutcomeSuccess = "success"
	armOutcomeTimeout = "timeout"
	armOutcomeError   = "error"
)

// armOutcome classifies an arming failure for the metric label.
func armOutcome(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timed out waiting") {
		return armOutcomeTimeout
	}
	return armOutcomeError
}
