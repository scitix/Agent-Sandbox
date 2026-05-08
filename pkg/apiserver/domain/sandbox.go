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

package domain

import (
	"time"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Sandbox is the core domain model for a claimed sandbox Pod.
//
// Fields with json tags are persisted to the history store (buntdb).
// Fields tagged json:"-" are runtime-only and never stored.
type Sandbox struct {
	// SandboxID is the unique identifier for the sandbox, generated at claim time.
	SandboxID string `json:"sandboxId"`
	// Namespace is the Kubernetes namespace of the sandbox Pod.
	Namespace string `json:"namespace"`
	// Team is the team label of the sandbox owner.
	Team string `json:"team,omitempty"`
	// User is the user label of the sandbox owner.
	User string `json:"user,omitempty"`
	// PoolName is the name of the pool the sandbox was claimed from.
	PoolName string `json:"poolName"`
	// PodName is the name of the sandbox Pod.
	PodName string `json:"podName"`
	// Metadata holds arbitrary key-value pairs provided at sandbox creation time.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Status is the current lifecycle status of the sandbox (e.g. Starting, Running, Completed, Failed, etc.).
	Status string `json:"status"`
	// ContainerImages maps container name → image reference (e.g. "python:3.10").
	ContainerImages map[string]string `json:"containerImages,omitempty"`

	// NodeName is the Kubernetes node the sandbox Pod was scheduled onto.
	NodeName string `json:"nodeName,omitempty"`
	// ContainerID is the runtime container ID (e.g. "docker://abc123…") of the first
	// container, captured when the sandbox first entered the Running state.
	// Populated from StableContainerStatuses in the inplace-update-state annotation.
	ContainerID string `json:"containerId,omitempty"`

	// CPU is the sum of all containers' CPU requests (raw K8s string, e.g. "8000m").
	CPU string `json:"cpu,omitempty"`
	// Memory is the sum of all containers' memory requests (raw K8s string, e.g. "8Gi").
	Memory string `json:"memory,omitempty"`

	// ClaimedAt is when the sandbox was claimed and the creation process started.
	ClaimedAt string `json:"claimedAt,omitempty"`
	// StartedAt is when the sandbox entered the Running state, or when a Completed record was persisted to the store.
	StartedAt string `json:"startedAt,omitempty"`
	// TerminatedAt is when the sandbox entered the Stopping state, or when a Failed (evicted/deleted) record was persisted to the store.
	TerminatedAt string `json:"terminatedAt,omitempty"`
	// RecycledAt is when the sandbox finished the Stopping→Idle recycle cycle,
	// or when a Failed (evicted/deleted) record was persisted to the store.
	RecycledAt string `json:"recycledAt,omitempty"`

	// DurationSeconds is the sandbox's wall-clock duration in seconds.
	// Runtime-only: computed at query time from startedAt/terminatedAt, never persisted.
	// Nil for Starting and Canceled states which lack a meaningful start-to-end interval.
	DurationSeconds *int64 `json:"-"`

	// Endpoints maps runtime name → SandboxEndpoint (URL + optional LogDir).
	// Runtime-only: populated from live pool spec, never persisted.
	Endpoints map[string]SandboxEndpoint `json:"-"`

	// StatusDetail holds structured diagnostics for Starting/Failed pods.
	// Runtime-only: derived from live Pod state, never persisted.
	StatusDetail *agentsv1alpha1.SandboxStatusDetail `json:"-"`

	// Historical fields (Completed/Failed/Canceled only):
	FailureReason  string `json:"failureReason,omitempty"`
	ExitCode       *int32 `json:"exitCode,omitempty"`
	FailureMessage string `json:"failureMessage,omitempty"`
}

// SandboxEndpoint carries the URL and optional log directory for a runtime endpoint.
type SandboxEndpoint struct {
	URL    string
	LogDir string // empty if not configured
}

// SandboxReadinessResult holds the aggregated readiness result for a sandbox.
type SandboxReadinessResult struct {
	SandboxID string
	Ready     bool
	Endpoints map[string]EndpointReadiness
}

// EndpointReadiness holds the readiness result for a single runtime endpoint.
type EndpointReadiness struct {
	Ready   bool
	Message string
}

// CreateSandboxInput carries all parameters needed to create a new sandbox.
type CreateSandboxInput struct {
	ClusterID       string // target cluster ID parsed from pool name prefix; empty means local
	PoolName        string
	Namespace       string
	Image           string
	ContainerImages map[string]string
	Labels          map[string]string
	Annotations     map[string]string
	Metadata        map[string]string
	StartupTimeout  time.Duration // 0 means no timeout
	IdleTimeout     time.Duration // 0 means no expiry
	// PostStartHooks are actions to run after the sandbox transitions Starting → Running.
	// Serialized to a pod annotation at claim time; consumed by the controller.
	PostStartHooks []PostStartHookAction
}

// PostStartHookAction describes a single action to execute after a sandbox becomes Running.
// Exactly one of Exec or HTTPPost should be set (mirrors k8s ProbeHandler style).
type PostStartHookAction struct {
	// Exec runs a shell command inside the sandbox container.
	Exec *ExecHookAction `json:"exec,omitempty"`
	// HTTPPost sends a POST request to an in-sandbox HTTP endpoint via the gateway.
	HTTPPost *HTTPPostHookAction `json:"httpPost,omitempty"`
}

// ExecHookAction runs a command inside the sandbox container.
type ExecHookAction struct {
	// Command is passed to sh -c inside the first container.
	Command string `json:"command"`
}

// HTTPPostHookAction sends a POST to a port/path inside the sandbox, routed through the gateway.
type HTTPPostHookAction struct {
	// Port is the target port of the in-sandbox service (required).
	Port int32 `json:"port"`
	// Path is the request path, e.g. "/init".
	Path string `json:"path"`
	// Body is an arbitrary JSON object serialized as the request body.
	Body map[string]any `json:"body,omitempty"`
	// Headers are extra HTTP headers to include (e.g. for authentication).
	Headers map[string]string `json:"headers,omitempty"`
}

// ListSandboxesFilter holds optional filters for listing sandboxes.
type ListSandboxesFilter struct {
	Namespace string
	PoolName  string
	Status    string // comma-separated multi-value supported (e.g. "Running,Failed")
	Team      string // when non-empty, only return sandboxes with this team label
	User      string // when non-empty, only return sandboxes with this user label
	Limit     int    // default 20, max 100; 0 means no limit
	Offset    int
}

// SandboxLogEntry is a single parsed log line from a container.
type SandboxLogEntry struct {
	Timestamp time.Time
	Container string
	Log       string
}

// SandboxLogs is the domain model returned by GetLogs.
type SandboxLogs struct {
	SandboxID   string
	Namespace   string
	PodName     string
	Entries     []SandboxLogEntry
	TotalBytes  int64
	Source      string // "live" | "runtime"
	RuntimeName string // set when Source == "runtime"
}

// GetLogsOptions controls log retrieval behaviour.
type GetLogsOptions struct {
	// Container restricts log retrieval to the named container.
	// Empty means all containers.
	Container string
	// Lines limits the response to the last N log entries.
	// 0 means return all entries.
	Lines int
	// Source selects log origin:
	// "" or "stdout" → container stdout/stderr (default)
	// "<runtimeName>" → read logDir via exec
	Source string
}

// DeleteSandboxResult is returned after a sandbox is released.
type DeleteSandboxResult struct {
	SandboxID string
	Namespace string
	PoolName  string
	PodName   string
	Status    string
}

// ExecTokenInfo carries the information stored in an exec token.
// It is returned by ValidateExecToken after successful token validation.
type ExecTokenInfo struct {
	SandboxID  string
	Namespace  string
	PodName    string
	Containers []string
}

// ExecCommandInput carries the parameters for executing a one-shot command in a sandbox.
type ExecCommandInput struct {
	Command        string
	TimeoutSeconds int // 0 means use default (30s)
}

// ExecCommandResult holds the output of a one-shot command execution.
type ExecCommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}
