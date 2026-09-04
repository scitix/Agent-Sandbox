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
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// rejectUnsupportedCreateFields refuses a create request that asks for
// something AgentBox will not do, instead of accepting it and doing something
// else.
//
// The create handler consumes templateID, metadata, envVars, timeout and
// network. Every other field of NewSandbox used to be dropped without a word:
// the request succeeded, the sandbox came back, and the behaviour quietly did
// not match what was asked for. For a human that is a puzzling afternoon; for
// an agent it is unrecoverable, because there is no signal to correct from —
// it will keep believing the sandbox pauses on timeout, or that its volume is
// mounted, and build on that.
//
// So each of them is a 400 that names the alternative. Returns nil when the
// request asks for nothing unsupported.
func rejectUnsupportedCreateFields(body *e2bgen.NewSandbox) *e2bgen.Error {
	if body == nil {
		return nil
	}

	if body.AutoPause != nil && *body.AutoPause {
		e := errRespCode(400, "autoPause is not supported: AgentBox has no pause, so a sandbox is killed "+
			"when its timeout expires. Extend the deadline instead by setting a longer timeout, or by "+
			"calling POST /sandboxes/{sandboxID}/timeout before it expires.")
		return &e
	}
	if body.AutoResume != nil && body.AutoResume.Enabled {
		e := errRespCode(400, "autoResume is not supported: it resumes a paused sandbox, and AgentBox has "+
			"no pause. A sandbox is either running or gone.")
		return &e
	}
	// autoPauseMemory only selects the snapshot kind for an auto-pause, so it is
	// meaningless on its own; rejecting it alongside autoPause would just be a
	// second error for the same mistake. It is accepted and ignored, and the
	// autoPause rejection above is what the caller sees.

	if body.Iam != nil && body.Iam.Tokens != nil && len(*body.Iam.Tokens) > 0 {
		e := errRespCode(400, "workload identity tokens (iam.tokens) are not supported. To give a sandbox "+
			"a credential it cannot read, store the credential with POST /secrets and reference it from a "+
			"network.rules header as ${e2b.secrets.<name>}; the egress proxy substitutes the real value "+
			"per request.")
		return &e
	}
	if body.Mcp != nil {
		e := errRespCode(400, "the mcp option is not supported. Start your MCP server inside the sandbox "+
			"as an ordinary command and reach it through the sandbox's host for that port.")
		return &e
	}
	if body.VolumeMounts != nil && len(*body.VolumeMounts) > 0 {
		e := errRespCode(400, "volumeMounts at create time are not supported: storage is declared once on "+
			"the SandboxEnv and mounted into every sandbox of that Env. Ask your platform admin or use the "+
			"AgentBox console.")
		return &e
	}

	// `secure` is accepted and ignored on purpose: it governs whether envd
	// requires its own access token, and AgentBox authenticates at the gateway
	// instead. Rejecting it would break every caller that passes the SDK
	// default, for no gain in correctness.
	return nil
}
