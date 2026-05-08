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
	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

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

// OnSandboxReady implements sandboxpool.SandboxReadyHook.
// It is called in a goroutine; errors are logged but never propagated.
func (r *Runner) OnSandboxReady(ctx context.Context, pod *corev1.Pod) {
	raw, ok := pod.Annotations[agentsv1alpha1.SandboxPostStartHooksAnnotationKey]
	if !ok || raw == "" {
		return
	}

	var hooks []apidomain.PostStartHookAction
	if err := json.Unmarshal([]byte(raw), &hooks); err != nil {
		klog.ErrorS(err, "poststarthooks: failed to decode hooks annotation",
			"pod", klog.KObj(pod))
		return
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

func (r *Runner) executeHook(ctx context.Context, pod *corev1.Pod, sandboxID string, hook apidomain.PostStartHookAction) error {
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
func (r *Runner) execHook(ctx context.Context, pod *corev1.Pod, action *apidomain.ExecHookAction) error {
	if r.clientset == nil || r.restConfig == nil {
		return fmt.Errorf("exec hook skipped: kubernetes clientset or rest config not configured")
	}
	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("exec hook skipped: pod has no containers")
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	container := pod.Spec.Containers[0].Name
	execReq := r.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"sh", "-c", action.Command},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(r.restConfig, http.MethodPost, execReq.URL())
	if err != nil {
		return fmt.Errorf("exec hook: failed to create executor: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	if err := executor.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
		Tty:    false,
	}); err != nil {
		return fmt.Errorf("exec hook: command failed: %w (stderr: %s)", err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

// httpPostHook sends a POST request through the gateway to an in-sandbox HTTP endpoint.
func (r *Runner) httpPostHook(ctx context.Context, sandboxID string, action *apidomain.HTTPPostHookAction) error {
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
