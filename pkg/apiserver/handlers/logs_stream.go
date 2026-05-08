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

// Package handlers implements the StrictServerInterface generated from the OpenAPI spec.
package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"

	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// streamLogEntry is the NDJSON entry type written by LogsStreamHandler.
// Field names are aligned with the external log service format.
type streamLogEntry struct {
	Timestamp     *time.Time `json:"_timestamp,omitempty"`
	ContainerName string     `json:"container_name,omitempty"`
	Log           string     `json:"log"`
	PodName       string     `json:"pod_name,omitempty"`
	NamespaceName string     `json:"namespace_name,omitempty"`
	NodeName      string     `json:"node_name,omitempty"`
}

// ndjsonMetaLine is the final line written to an NDJSON log stream to indicate stream completion.
// The _meta field acts as a discriminator (external log entries never have this field).
type ndjsonMetaLine struct {
	Meta      bool   `json:"_meta"`  // always true
	Source    string `json:"source"` // "live" | "cached" | "runtime"
	Truncated bool   `json:"truncated"`
	PodName   string `json:"pod_name,omitempty"`
}

// ndjsonLineWriter adapts an io.Writer for use as remotecommand.StreamOptions.Stdout/Stderr.
// It buffers partial lines across Write() calls and encodes each complete line as a
// JSON "entry" object, keeping per-write memory usage at O(line length).
type ndjsonLineWriter struct {
	w             io.Writer
	enc           *json.Encoder
	buf           bytes.Buffer
	containerName string
	podName       string
	namespaceName string
	nodeName      string
}

func newNdjsonLineWriter(w io.Writer, containerName, podName, namespaceName, nodeName string) *ndjsonLineWriter {
	return &ndjsonLineWriter{
		w:             w,
		enc:           json.NewEncoder(w),
		containerName: containerName,
		podName:       podName,
		namespaceName: namespaceName,
		nodeName:      nodeName,
	}
}

// Write implements io.Writer. It scans for newlines and JSON-encodes each complete line.
func (lw *ndjsonLineWriter) Write(p []byte) (int, error) {
	lw.buf.Write(p) //nolint:errcheck // bytes.Buffer.Write never returns an error
	for {
		line, err := lw.buf.ReadString('\n')
		if err != nil {
			// Incomplete line — put it back and wait for the next chunk.
			if len(line) > 0 {
				lw.buf.WriteString(line) //nolint:errcheck
			}
			break
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		now := time.Now().UTC()
		entry := streamLogEntry{
			Timestamp:     &now,
			ContainerName: lw.containerName,
			Log:           trimmed,
			PodName:       lw.podName,
			NamespaceName: lw.namespaceName,
			NodeName:      lw.nodeName,
		}
		if encErr := lw.enc.Encode(entry); encErr != nil {
			return len(p), encErr
		}
		if f, ok := lw.w.(http.Flusher); ok {
			f.Flush()
		}
	}
	return len(p), nil
}

// Flush writes any buffered partial line as a final entry.
func (lw *ndjsonLineWriter) Flush() {
	if lw.buf.Len() == 0 {
		return
	}
	trimmed := strings.TrimRight(lw.buf.String(), "\r\n")
	lw.buf.Reset()
	if trimmed == "" {
		return
	}
	now := time.Now().UTC()
	entry := streamLogEntry{
		Timestamp:     &now,
		ContainerName: lw.containerName,
		Log:           trimmed,
		PodName:       lw.podName,
		NamespaceName: lw.namespaceName,
		NodeName:      lw.nodeName,
	}
	_ = lw.enc.Encode(entry)
}

// LogsStreamHandler returns a gin.HandlerFunc for NDJSON log streaming.
// Route: GET /v1/sandboxes/:sandboxId/logs/stream
//
// Query parameters:
//   - lines int  (0 = all, default 0)
//   - source string  ("" or "stdout" for pod stdout; runtime name for runtime log files)
//
// Response format: application/x-ndjson, each line is a JSON object.
// Entry lines: {"_timestamp":"...","container_name":"...","log":"...","pod_name":"...","namespace_name":"...","node_name":"..."}
// Final meta line: {"_meta":true,"source":"live|runtime","truncated":false,"pod_name":"..."}
func LogsStreamHandler(
	sandboxSvc service.SandboxService,
	clientset kubernetes.Interface,
	restCfg *rest.Config,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		sandboxID := c.Param("sandboxId")
		if sandboxID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sandboxId is required"})
			return
		}

		// Parse query parameters.
		lines := 0
		if linesStr := c.Query("lines"); linesStr != "" {
			if n, err := strconv.Atoi(linesStr); err == nil && n >= 0 {
				lines = n
			}
		}
		source := c.Query("source")

		auth := middleware.AuthFromContext(c)
		namespace := auth.Namespace

		// Set HTTP headers for server-sent streaming (NDJSON).
		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Status(http.StatusOK)
		// Flush HTTP headers immediately so upstream proxies (nginx, envoy)
		// see X-Accel-Buffering:no before any log data arrives.  Without
		// this early flush, small payloads (2-3 log lines) can sit unseen
		// in proxy buffers until a size threshold is crossed.
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}

		enc := json.NewEncoder(c.Writer)
		flush := func() {
			if f, ok := c.Writer.(http.Flusher); ok {
				f.Flush()
			}
		}

		// Helper to write the terminal metadata line
		writeMeta := func(src string, truncated bool, podName string) {
			_ = enc.Encode(ndjsonMetaLine{
				Meta:      true,
				Source:    src,
				Truncated: truncated,
				PodName:   podName,
			})
			flush()
		}

		ctx := c.Request.Context()

		// ── 1. Runtime log source (File via exec) ──────────────────────────────
		if source != "" && source != "stdout" {
			if clientset == nil || restCfg == nil {
				_ = enc.Encode(streamLogEntry{
					Log: "[agentbox] exec is not available: kubernetes clientset not configured",
				})
				writeMeta("runtime", false, "")
				return
			}
			streamRuntimeLogsToWriter(ctx, c.Writer, clientset, restCfg, sandboxSvc, namespace, sandboxID, source, lines, enc)
			flush()
			return
		}

		// ── 2. Stdout log source (Pod Logs) ────────────────────────────────────
		// Try fetching live pod logs.
		if clientset != nil {
			podName, containerName, liveErr := findLiveSandboxPod(ctx, sandboxSvc, namespace, sandboxID)
			if liveErr == nil {
				logOpts := buildPodLogOpts(containerName, lines)
				truncated, streamErr := streamPodLogsToEnc(ctx, clientset, namespace, podName, logOpts, c.Writer, enc)

				// Ignore context canceled errors (happens when frontend disconnects normally)
				if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
					klog.V(2).InfoS("live log stream error", "sandboxId", sandboxID, "error", streamErr)
					// Push the error to the stream so the frontend UI can display it
					now := time.Now().UTC()
					_ = enc.Encode(streamLogEntry{
						Timestamp:     &now,
						ContainerName: "system",
						Log:           fmt.Sprintf("[agentbox] pod stream interrupted: %v", streamErr),
					})
				}
				writeMeta("live", truncated, podName)
				return
			}
		}

		// No live pod found — nothing to stream.
		writeMeta("live", false, "")
	}
}

// findLiveSandboxPod returns the pod name and primary container name for a live (non-terminated) sandbox.
func findLiveSandboxPod(ctx context.Context, sandboxSvc service.SandboxService, namespace, sandboxID string) (podName, containerName string, err error) {
	result, appErr := sandboxSvc.Get(ctx, namespace, sandboxID)
	if appErr != nil {
		return "", "", fmt.Errorf("sandbox not found: %s", appErr.Message)
	}
	if result.Status == "Completed" || result.Status == "Failed" || result.Status == "Canceled" {
		return "", "", fmt.Errorf("sandbox is terminated")
	}
	if result.PodName == "" {
		return "", "", fmt.Errorf("sandbox has no pod")
	}
	// Pick the first container name from ContainerImages (sandbox pods typically have one main container).
	for name := range result.ContainerImages {
		containerName = name
		break
	}
	return result.PodName, containerName, nil
}

// buildPodLogOpts builds PodLogOptions with Follow set to true for continuous HTTP streaming.
func buildPodLogOpts(container string, tailLines int) *corev1.PodLogOptions {
	opts := &corev1.PodLogOptions{
		Container:  container,
		Timestamps: true,
		Follow:     true, // CRITICAL: Enables continuous streaming (tail -f behavior)
	}
	if tailLines > 0 {
		n := int64(tailLines)
		opts.TailLines = &n
	}
	return opts
}

// streamPodLogsToEnc streams pod logs via the Kubernetes API to the JSON encoder,
// writing one StreamLogEntry per line. Returns (truncated bool, err error).
func streamPodLogsToEnc(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, podName string,
	logOpts *corev1.PodLogOptions,
	w io.Writer,
	enc *json.Encoder,
) (truncated bool, err error) {
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, logOpts)
	rc, streamErr := req.Stream(ctx)
	if streamErr != nil {
		return false, streamErr
	}
	defer rc.Close() //nolint:errcheck

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		entry := parseStreamLogLineBytes(raw, logOpts.Container)
		if encErr := enc.Encode(entry); encErr != nil {
			return truncated, encErr
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	return false, scanner.Err()
}

// parseStreamLogLineBytes parses a raw log line into a streamLogEntry, extracting the RFC3339 timestamp.
func parseStreamLogLineBytes(raw []byte, containerName string) streamLogEntry {
	entry := streamLogEntry{
		ContainerName: containerName,
	}
	str := string(raw)
	for i := 0; i < len(str); i++ {
		if str[i] == ' ' {
			ts, parseErr := time.Parse(time.RFC3339Nano, str[:i])
			if parseErr == nil {
				t := ts.UTC()
				entry.Timestamp = &t
				if i+1 < len(str) {
					entry.Log = str[i+1:]
				}
				return entry
			}
			break
		}
	}
	entry.Log = str
	return entry
}

// streamRuntimeLogsToWriter streams runtime log file content via SPDY exec directly to w.
func streamRuntimeLogsToWriter(
	ctx context.Context,
	w http.ResponseWriter,
	clientset kubernetes.Interface,
	restCfg *rest.Config,
	sandboxSvc service.SandboxService,
	namespace, sandboxID, runtimeName string,
	lines int,
	enc *json.Encoder,
) {
	// Get sandbox info to find the pod and logDir.
	result, appErr := sandboxSvc.Get(ctx, namespace, sandboxID)
	if appErr != nil {
		_ = enc.Encode(streamLogEntry{Log: fmt.Sprintf("[agentbox] sandbox error: %s", appErr.Message)})
		writeMetaToEnc(enc, w, result.PodName)
		return
	}
	if result.PodName == "" {
		_ = enc.Encode(streamLogEntry{Log: "[agentbox] sandbox has no pod (may be terminated)"})
		writeMetaToEnc(enc, w, result.PodName)
		return
	}

	// Look up logDir from sandbox endpoints.
	logDir := ""
	for name, ep := range result.Endpoints {
		if name == runtimeName {
			logDir = ep.LogDir
			break
		}
	}
	if logDir == "" {
		_ = enc.Encode(streamLogEntry{Log: fmt.Sprintf("[agentbox] runtime %q not found or has no logDir", runtimeName)})
		writeMetaToEnc(enc, w, result.PodName)
		return
	}

	// Build the shell command for continuous tailing (tail -f).
	// We use 2>&1 to redirect stderr to stdout so we can catch "file not found" errors in the stream.
	var fullCmd string
	if lines > 0 {
		fullCmd = fmt.Sprintf("tail -n %d -f %s 2>&1", lines, logDir)
	} else {
		// tail -n +1 outputs the entire file starting from line 1, then follows it.
		fullCmd = fmt.Sprintf("tail -n +1 -f %s 2>&1", logDir)
	}

	// NOTE: If your sandbox uses a specific sidecar container for logs, set Container: runtimeName
	// Otherwise, leave it empty to use the first/primary container.
	execReq := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(result.PodName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "", // Use specific container name here if needed
			Command:   []string{"sh", "-c", fullCmd},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true, // Capture stderr
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, execErr := remotecommand.NewSPDYExecutor(restCfg, http.MethodPost, execReq.URL())
	if execErr != nil {
		klog.ErrorS(execErr, "Failed to create SPDY executor for log stream", "sandboxId", sandboxID)
		now := time.Now().UTC()
		_ = enc.Encode(streamLogEntry{
			Timestamp: &now, Log: fmt.Sprintf("[agentbox] failed to create exec session: %v", execErr),
		})
		writeMetaToEnc(enc, w, result.PodName)
		return
	}

	lineWriter := newNdjsonLineWriter(w, runtimeName, result.PodName, result.Namespace, result.NodeName)

	// StreamWithContext blocks until the command completes or the context is canceled (frontend disconnects).
	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: lineWriter,
		Stderr: lineWriter, // Route Stderr to the same JSON encoder
		Tty:    false,
	})

	// Flush any remaining buffered partial line.
	lineWriter.Flush()

	// Handle explicit SPDY stream errors (e.g. 30s Dial Timeout)
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		klog.V(2).InfoS("runtime log stream ended with error", "sandboxId", sandboxID, "runtimeName", runtimeName, "error", streamErr)

		// Ensure the frontend receives the error instead of hanging silently
		now := time.Now().UTC()
		_ = enc.Encode(streamLogEntry{
			Timestamp:     &now,
			ContainerName: "system",
			Log:           fmt.Sprintf("[agentbox] exec stream failed: %v", streamErr),
		})
	}

	writeMetaToEnc(enc, w, result.PodName)
}

// Helper function to write the meta line and flush
func writeMetaToEnc(enc *json.Encoder, w http.ResponseWriter, podName string) {
	_ = enc.Encode(ndjsonMetaLine{
		Meta:      true,
		Source:    "runtime",
		Truncated: false,
		PodName:   podName,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
