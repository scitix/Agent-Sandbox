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

package approval

import (
	"net/http"
	"strings"
)

// Op is what one route means to a person being asked about it.
type Op struct {
	// ID is what a grant is written against, so it is the unit a person is
	// really deciding about: "let this agent create environments", not "let it
	// call PATCH on that route".
	ID string
	// OnceOnly withholds the session and key scopes. Two kinds of call earn it:
	// those that destroy something, and those that mint a credential. Neither is
	// something a person should be able to authorise in advance and in bulk.
	OnceOnly bool
	// Summary is what the approval card says. Written for the person deciding,
	// not for the log: it names the thing, because "create a pool" and "create a
	// pool in prod" are different questions.
	Summary string
}

// gated maps "METHOD /gin/route/:pattern" to the act it performs.
//
// **Native routes only.** The E2B-compatible surface is not gated at all, and
// that is the line the whole feature is drawn along:
//
//   - `abx` — the CLI an agent drives to operate the PLATFORM — speaks the
//     native API and nothing else. Gating native is therefore exactly gating
//     the agent's reach over environments, pools, keys and templates.
//   - The E2B surface is what a sandbox's own SDK speaks. It is a published,
//     third-party-compatible contract: real E2B client code, written against
//     e2b.dev, is expected to work against it unchanged. Injecting a 428 that
//     no E2B client has ever heard of would break every such program, and the
//     ones it would break hardest are the ones doing exactly what the sandbox
//     is for.
//
// The line also removes two hazards that gating E2B would have created: the
// keepalive (`POST /sandboxes/{id}/refreshes`) fires on a timer for the life of
// every sandbox and must never wait on a human, and the platform arms a
// sandbox's own vault through `/secrets` while starting it, which would have
// deadlocked the start against an approval nobody could grant yet.
//
// Keyed on the ROUTE TEMPLATE (`c.FullPath()`), never on the concrete URL: a
// map keyed on the latter would be unbounded, and a prefix match over it would
// be one path-traversal away from letting an ungated route look gated.
//
// Anything absent from this map is ungated. That direction is chosen so a new
// route ships closed-to-nothing rather than closed-to-everything: adding an
// endpoint should not silently start prompting people, and the omission is
// visible the moment someone asks why their new write is not gated. The
// deliberate omissions are listed in `exempt` below with their reasons.
var gated = map[string]Op{
	"POST /v1/sandboxes":                                  {ID: "sandbox.create", Summary: "Start a sandbox"},
	"DELETE /v1/sandboxes/:sandboxId":                     {ID: "sandbox.delete", OnceOnly: true, Summary: "Delete a sandbox"},
	"PUT /v1/sandboxes/:sandboxId/timeout":                {ID: "sandbox.update", Summary: "Change a sandbox's timeout"},
	"POST /v1/sandboxes/:sandboxId/exec":                  {ID: "sandbox.exec", Summary: "Run a command in a sandbox"},
	"POST /v1/envs":                                       {ID: "env.create", Summary: "Create an environment"},
	"PATCH /v1/envs/:name":                                {ID: "env.update", Summary: "Change an environment"},
	"DELETE /v1/envs/:name":                               {ID: "env.delete", OnceOnly: true, Summary: "Delete an environment"},
	"POST /v1/envs/:name/sandboxpools":                    {ID: "pool.create", Summary: "Add a warm pool"},
	"PUT /v1/envs/:name/sandboxpools/:poolName":           {ID: "pool.update", Summary: "Change a warm pool"},
	"DELETE /v1/envs/:name/sandboxpools/:poolName":        {ID: "pool.delete", OnceOnly: true, Summary: "Delete a warm pool"},
	"PUT /v1/envs/:name/autoscaling/groups/:groupName":    {ID: "autoscaling.update", Summary: "Change an autoscaling group"},
	"DELETE /v1/envs/:name/autoscaling/groups/:groupName": {ID: "autoscaling.delete", OnceOnly: true, Summary: "Delete an autoscaling group"},

	// Minting or revoking a credential is never covered by a standing grant:
	// a key made without anyone looking outlives the session that made it, and
	// an exec token is a shell.
	"POST /v1/api-keys":                        {ID: "apikey.create", OnceOnly: true, Summary: "Create an API key"},
	"DELETE /v1/api-keys/:name":                {ID: "apikey.delete", OnceOnly: true, Summary: "Delete an API key"},
	"POST /v1/sandboxes/:sandboxId/exec-token": {ID: "sandbox.execToken", OnceOnly: true, Summary: "Mint a terminal token for a sandbox"},
}

// exempt records the native writes that are deliberately NOT gated, and why.
// It is prose rather than code because nothing reads it — but each of these is
// a decision someone will otherwise re-litigate from scratch.
//
//	Everything under /v1/admin
//	    Already requires an admin key, and an admin key is not a credential we
//	    hand to an agent. If that changes, they belong in `gated`.
//
//	GET /v1/sandboxes/:sandboxId/terminal, .../logs/stream
//	    Reads, and the terminal authenticates with an exec token rather than the
//	    API key. The act worth gating is minting that token, which is in `gated`
//	    as `sandbox.execToken`.
//
// The whole E2B-compatible surface is exempt by construction — see the note on
// `gated` above.
const exemptRationale = ""

var _ = exemptRationale

// Lookup returns what this route does, and whether it is gated at all.
func Lookup(method, routeTemplate string) (Op, bool) {
	op, ok := gated[method+" "+strings.TrimSuffix(routeTemplate, "/")]
	return op, ok
}

// IsWrite reports whether a method changes anything. Used only to skip the
// lookup early; the map is the authority on what is gated.
func IsWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}
