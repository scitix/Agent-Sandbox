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

// Package protocol defines the WebSocket sync protocol shared between the
// Worker (pkg/apiserver) and the ws-proxy (cmd/wsproxy).
//
// Having a single source of truth prevents type drift (e.g. json.RawMessage vs
// []byte) and makes the wire format an explicit, versioned contract.
package protocol

import "encoding/json"

// Frame type constants are organised by direction and resource type:
//
//	ws-proxy → Worker push:    FrameKey*/FrameTemplate*/FrameClusterConfig*
//	Worker → ws-proxy request: FrameKeyCreate/FrameKeyDelete/FrameTemplate{Create,Update,Delete}
//	ws-proxy → Worker response: FrameKey*Resp/FrameTemplate*Resp

// ── Frame type constants ──────────────────────────────────────────────────────

// FrameType is the discriminant field ("type") on every sync frame.
type FrameType string

const (
	// ── API Key: ws-proxy → Worker (push) ────────────────────────────────────
	FrameKeySync       FrameType = "key_sync"
	FrameKeyDeleteSync FrameType = "key_delete_sync"
	FrameKeySnapshot   FrameType = "key_snapshot"

	// ── API Key: ws-proxy → Worker (responses) ───────────────────────────────
	FrameKeyCreateResp FrameType = "key_create_resp"
	FrameKeyDeleteResp FrameType = "key_delete_resp"

	// ── API Key: Worker → ws-proxy (requests) ────────────────────────────────
	FrameKeyCreate FrameType = "key_create"
	FrameKeyDelete FrameType = "key_delete"

	// ── SandboxTemplate: ws-proxy → Worker (push) ────────────────────────────
	FrameTemplateSnapshot   FrameType = "template_snapshot"
	FrameTemplateSync       FrameType = "template_sync"
	FrameTemplateDeleteSync FrameType = "template_delete_sync"

	// ── SandboxTemplate: ws-proxy → Worker (responses) ───────────────────────
	FrameTemplateCreateResp FrameType = "template_create_resp"
	FrameTemplateUpdateResp FrameType = "template_update_resp"
	FrameTemplateDeleteResp FrameType = "template_delete_resp"

	// ── SandboxTemplate: Worker → ws-proxy (requests) ────────────────────────
	FrameTemplateCreate FrameType = "template_create"
	FrameTemplateUpdate FrameType = "template_update"
	FrameTemplateDelete FrameType = "template_delete"

	// ── ClusterConfig: ws-proxy → Worker (push) ──────────────────────────────
	//
	// FrameClusterConfigSnapshot is sent once on every new WS connection to
	// deliver the full current cluster list.
	// FrameClusterConfigSync is broadcast whenever the manager cluster's
	// clusters.yaml changes (e.g. after a 30-second reload tick).
	FrameClusterConfigSnapshot FrameType = "cluster_config_snapshot"
	FrameClusterConfigSync     FrameType = "cluster_config_sync"
)

// ── Shared frame type ─────────────────────────────────────────────────────────

// Frame is the single wire-format envelope used for all bidirectional sync
// messages between Worker and ws-proxy.  Both sides marshal/unmarshal this
// same struct, eliminating the previous json.RawMessage vs []byte drift.
type Frame struct {
	// ── Routing ──────────────────────────────────────────────────────────────
	ID   string    `json:"id,omitempty"` // correlation ID for request/response pairing
	Type FrameType `json:"type"`

	// ── API Key metadata (key_sync / key_snapshot item) ───────────────────────
	TokenHash   string `json:"tokenHash,omitempty"`
	HashPrefix  string `json:"hashPrefix,omitempty"`
	Name        string `json:"name,omitempty"` // secret name (delete / sync)
	Namespace   string `json:"namespace,omitempty"`
	Role        string `json:"role,omitempty"`
	User        string `json:"user,omitempty"`
	Team        string `json:"team,omitempty"`
	QuotaURL    string `json:"quotaURL,omitempty"`
	Description string `json:"description,omitempty"`
	IssuedAt    string `json:"issuedAt,omitempty"`  // RFC3339
	ExpiresAt   string `json:"expiresAt,omitempty"` // RFC3339

	// ── Snapshot batch (key_snapshot / template_snapshot) ────────────────────
	Items []Frame `json:"items,omitempty"`

	// ── Response fields (ws-proxy → Worker) ──────────────────────────────────
	OK         bool   `json:"ok,omitempty"`
	RawToken   string `json:"rawToken,omitempty"`
	KeyID      string `json:"keyId,omitempty"`
	Error      string `json:"error,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`

	// ── SandboxTemplate fields ────────────────────────────────────────────────
	// TemplateFull carries the complete serialised SandboxTemplate object
	// (ObjectMeta + Spec). It is used in all directions: create/update requests
	// (Worker → ws-proxy), sync push (ws-proxy → Worker), and snapshots.
	// Delete frames reuse the top-level Name field for the template name.
	TemplateFull json.RawMessage `json:"templateFull,omitempty"`

	// ── ClusterConfig fields ──────────────────────────────────────────────────
	// ConfigSnapshot carries the full serialised ClusterConfig snapshot
	// (clusters + hostAliases + any future top-level fields) for
	// cluster_config_sync and cluster_config_snapshot frames.
	//
	// Using an opaque json.RawMessage here keeps the protocol layer free from
	// importing the cluster package and lets the Manager add new top-level
	// fields without bumping this struct. The Worker unmarshals it into
	// cluster.ClusterConfig on receipt.
	//
	// Workers MUST treat a zero-length ConfigSnapshot as a no-op (do not
	// clear config) so a transient empty push cannot erase a populated
	// ConfigMap on disk.
	ConfigSnapshot json.RawMessage `json:"configSnapshot,omitempty"`
}

// ── Key request / response helpers ───────────────────────────────────────────

// CreateKeyRequest carries the parameters for a key_create request
// sent from Worker to ws-proxy.
type CreateKeyRequest struct {
	Namespace   string `json:"namespace"`
	User        string `json:"user"`
	Team        string `json:"team"`
	Role        string `json:"role"`
	Description string `json:"description"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	// Import/promote mode: when TokenHash + HashPrefix are set, the ws-proxy
	// stores the existing hash instead of generating a new token.
	TokenHash  string `json:"tokenHash,omitempty"`
	HashPrefix string `json:"hashPrefix,omitempty"`
	IssuedAt   string `json:"issuedAt,omitempty"`
	QuotaURL   string `json:"quotaURL,omitempty"`
	// RawToken carries the plaintext token for promote operations so that
	// other Workers can also store the recoverable key value.
	RawToken string `json:"rawToken,omitempty"`
}

// CreateKeyResponse holds the parsed result from a key_create_resp frame.
type CreateKeyResponse struct {
	RawToken   string
	KeyID      string
	TokenHash  string
	HashPrefix string
	IssuedAt   string
}

// ── Error helpers ─────────────────────────────────────────────────────────────

// HTTPError carries an HTTP status code from a ws-proxy error response frame.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }
