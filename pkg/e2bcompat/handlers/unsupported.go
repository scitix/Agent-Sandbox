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

package handlers

import (
	"encoding/json"
	"net/http"

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
)

// AgentBox implements a subset of the E2B API. This file is how the rest of it
// answers.
//
// Two things matter about that answer, and neither was true before.
//
// It has to be HTTP 501. The E2B spec declares no 501 for these operations, so
// the generated response types only offer 500 — which reads as "we broke" and
// invites a retry. The SDK does not retry 5xx (its retries live at the
// connection layer), so the cost of the wrong code is not wasted requests but a
// wrong diagnosis, and an agent that keeps trying because 500 sounds transient.
// 501 says "this will never work here" in a way both a human and a model read
// correctly. Getting it requires implementing the generated response interface
// ourselves: each operation's interface has a distinct method name, so one type
// carrying all of them satisfies every one, and a missing method is a compile
// error rather than a silently un-migrated endpoint.
//
// And the message has to be actionable. The SDK surfaces errors as
// fmt.Sprintf("%d: %s", status, message) — the message is the entire signal
// that reaches the caller, and increasingly the caller is a model choosing what
// to do next. So every message below says what is not supported, why, and what
// to use instead. "not supported in AgentBox" satisfied none of those.

// Categories, for the metric label. They answer different questions: an
// architectural refusal will never become supported and the caller should stop
// asking; a platform one means the operation exists but lives elsewhere; an
// unimplemented one is a roadmap gap worth counting.
const (
	catArchitectural = "architectural"
	catPlatform      = "platform"
	catUnimplemented = "unimplemented"
)

// Message catalogue. Grouped so operations that share a reason share a string —
// pause and resume fail for the same reason and should say the same thing.
const (
	msgPauseResume = "AgentBox has no pause/resume: a sandbox is a claimed Pod from a pre-warmed pool, " +
		"with no snapshot storage behind it. Keep it alive with POST /sandboxes/{sandboxID}/timeout, " +
		"or create a new sandbox from the same template."

	msgSnapshots = "Snapshots are not supported: AgentBox sandboxes are Kubernetes Pods, not Firecracker " +
		"microVMs, so there is no memory or filesystem image to capture. Persist state to a volume " +
		"declared on the SandboxEnv instead."

	msgFork = "Fork is not supported because it requires a snapshot. Create a second sandbox from the " +
		"same template and copy the files you need."

	msgTemplateBuild = "AgentBox does not build templates through this API. A template here is a SandboxEnv " +
		"backed by a prebuilt container image: build and push the image with your own CI, then create the " +
		"SandboxEnv in the AgentBox console. GET /templates and GET /templates/{templateID} are supported."

	msgTemplateTags = "Template tags are not supported: an AgentBox template is a SandboxEnv, which is " +
		"versioned by the image its pools run rather than by tags. Point the SandboxEnv at a new image tag " +
		"in the AgentBox console."

	msgTemplateAlias = "Alias lookup is not implemented. GET /templates/{templateID} accepts a SandboxEnv " +
		"name directly, so pass the name you would have aliased."

	msgTemplateListV2 = "GET /v2/templates is not implemented. Use GET /templates, which lists the same " +
		"SandboxEnvs."

	msgVolumes = "Per-sandbox volumes cannot be created here. Storage is declared once on the SandboxEnv, " +
		"as a PVC mounted into every sandbox of that Env — ask your platform admin or use the AgentBox console."

	msgClusterAdmin = "Cluster and team administration is not exposed through the E2B-compatible API. " +
		"Use the AgentBox native API or console."

	msgAccessTokens = "Access tokens are not supported; AgentBox authenticates with API keys only. " +
		"Send \"X-API-Key: agbx_...\" (create one with POST /api-keys)."

	msgAPIKeyRename = "Renaming an API key is not supported. Delete it with DELETE /api-keys/{apiKeyID} " +
		"and create a new one."

	msgSecrets = "Secrets are not enabled in this AgentBox deployment. Without the credential vault there " +
		"is nowhere to store a value that ${e2b.secrets.<name>} could resolve to, so network.rules cannot " +
		"be used either. Ask your platform admin to enable it."

	msgLogs = "Sandbox log retrieval is not enabled in this AgentBox deployment. Use the output returned " +
		"by the command call itself, or read the Pod logs in the AgentBox console."

	msgMetrics = "Sandbox metrics are not enabled in this AgentBox deployment. Use the AgentBox console, " +
		"or ask your platform admin to configure a metrics backend for this cluster."
)

// unsupported implements every generated response interface for an operation
// AgentBox does not serve. Construct it with the operation name so the metric
// records which endpoint was asked for.
type unsupported struct {
	op       string
	category string
	msg      string
}

func unsupportedOp(op, category, msg string) unsupported {
	return unsupported{op: op, category: category, msg: msg}
}

// write emits the 501 body and counts the call. The counter is the point of
// keeping the operation name around: it turns "what do callers actually want
// that we do not have" from a guess into a query.
func (u unsupported) write(w http.ResponseWriter) error {
	pkgmetrics.E2BUnsupportedTotal.WithLabelValues(u.op, u.category).Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	return json.NewEncoder(w).Encode(e2bgen.Error{
		Code:    http.StatusNotImplemented,
		Message: u.msg,
	})
}

// Generated response-interface methods. One per unsupported operation; each
// satisfies that operation's generated response interface.

func (u unsupported) VisitPostSandboxesSandboxIDPauseResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostSandboxesSandboxIDResumeResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostSandboxesSandboxIDSnapshotsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetSnapshotsResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitPostSandboxesSandboxIDForkResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetV2TemplatesResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitPostTemplatesResponse(w http.ResponseWriter) error  { return u.write(w) }
func (u unsupported) VisitPostTemplatesTemplateIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPatchTemplatesTemplateIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitDeleteTemplatesTemplateIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostTemplatesTemplateIDBuildsBuildIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetTemplatesTemplateIDBuildsBuildIDLogsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetTemplatesTemplateIDBuildsBuildIDStatusResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetTemplatesTemplateIDFilesHashResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostV2TemplatesResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitPatchV2TemplatesTemplateIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostV2TemplatesTemplateIDBuildsBuildIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostV3TemplatesResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitGetTemplatesTemplateIDTagsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostTemplatesTagsResponse(w http.ResponseWriter) error   { return u.write(w) }
func (u unsupported) VisitDeleteTemplatesTagsResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitGetTemplatesAliasesAliasResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetVolumesResponse(w http.ResponseWriter) error         { return u.write(w) }
func (u unsupported) VisitPostVolumesResponse(w http.ResponseWriter) error        { return u.write(w) }
func (u unsupported) VisitGetVolumesVolumeIDResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitDeleteVolumesVolumeIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetNodesResponse(w http.ResponseWriter) error        { return u.write(w) }
func (u unsupported) VisitGetNodesNodeIDResponse(w http.ResponseWriter) error  { return u.write(w) }
func (u unsupported) VisitPostNodesNodeIDResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitGetTeamsResponse(w http.ResponseWriter) error        { return u.write(w) }
func (u unsupported) VisitGetTeamsTeamIDMetricsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetTeamsTeamIDMetricsMaxResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetAdminSandboxesRunningCountsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostAdminTeamsTeamIDBuildsCancelResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostAdminTeamsTeamIDApiKeysResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitDeleteAdminTeamsTeamIDApiKeysApiKeyIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostAdminTeamsTeamIDSandboxesKillResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPostAccessTokensResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitDeleteAccessTokensAccessTokenIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitPatchApiKeysApiKeyIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetSecretsResponse(w http.ResponseWriter) error          { return u.write(w) }
func (u unsupported) VisitGetSecretsSecretIDResponse(w http.ResponseWriter) error  { return u.write(w) }
func (u unsupported) VisitPostSecretsResponse(w http.ResponseWriter) error         { return u.write(w) }
func (u unsupported) VisitPostSecretsSecretIDResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitDeleteSecretsSecretIDResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetSandboxesSandboxIDLogsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetV2SandboxesSandboxIDLogsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unsupported) VisitGetSandboxesMetricsResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unsupported) VisitGetSandboxesSandboxIDMetricsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
