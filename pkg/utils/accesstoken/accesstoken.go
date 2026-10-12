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

// Package accesstoken mints the per-sandbox tokens that secure the data plane,
// and names the headers and envd control paths that go with them.
//
// Two separate secrets protect two separate surfaces of a sandbox:
//
//   - the envd token guards envd itself (port 49983): the file API and the
//     process/filesystem RPCs. envd enforces it natively once it is set — every
//     RPC must carry X-Access-Token, and the signing-based /files paths need the
//     same token to produce a valid signature.
//   - the traffic token guards the user's own service ports when the sandbox was
//     created without public access. The gateway enforces it: a request to a
//     non-envd port must carry e2b-traffic-access-token.
//
// Both are a deterministic HMAC-SHA256 over the sandbox's raw UUID, so the
// operator (which delivers the envd token to envd via /init), the E2B API (which
// returns both tokens to the SDK), and the gateway (which validates the traffic
// token) all derive the same value from the shared secret with nothing stored
// and nothing to synchronise. The formulas match upstream e2b-dev/infra
// (packages/api/internal/sandbox/sandbox_envd_secret.go) so the stock E2B SDK,
// which simply echoes whatever tokens the create/connect response gave it, works
// unmodified.
//
// The canonical input is always the RAW sandbox UUID, never a cluster-prefixed
// ID: the pod annotation and the gateway (after it strips its local prefix) both
// use the raw UUID, so any minter that hashes a prefixed ID would silently
// disagree with the other two for cross-cluster sandboxes.
package accesstoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"path"
)

const (
	// EnvdHeader is the header envd checks for its access token.
	EnvdHeader = "X-Access-Token"
	// TrafficHeader is the header the gateway checks for the traffic token on
	// non-envd ports.
	TrafficHeader = "e2b-traffic-access-token"
	// EnvdPort is the in-sandbox envd listener. Traffic to it is authenticated
	// by envd itself (EnvdHeader); every other port is a user service guarded by
	// the traffic token.
	EnvdPort int32 = 49983

	// sandboxTrafficPrefix namespaces the traffic token's input so it can never
	// collide with the envd token for the same sandbox. Matches upstream.
	sandboxTrafficPrefix = "sandbox-traffic-"
)

// internalEnvdPaths are envd control routes the orchestrator reaches directly at
// the pod IP and that no request arriving through the public gateway may reach.
// /init is the one that matters most: before arming delivers the access token,
// envd accepts a first tokenless /init that SETS a token, so an unauthenticated
// caller racing one through the gateway could claim a sandbox's envd. Blocking
// these at the gateway closes that window; the legitimate /init is dialled
// straight to the pod and is unaffected.
//
// Mirrors e2b-dev/infra packages/orchestrator/pkg/sandbox/envd/internal_routes.
var internalEnvdPaths = map[string]struct{}{
	"/init":     {},
	"/freeze":   {},
	"/unfreeze": {},
	"/fsfreeze": {},
	"/fsthaw":   {},
	"/collapse": {},
	"/upgrade":  {},
}

// IsInternalEnvdPath reports whether requestPath addresses an envd control
// route. requestPath MUST be the decoded path (http.Request.URL.Path), never
// RawPath/EscapedPath/RequestURI: passing a still-escaped path would let
// "/%69nit" slip past this check and reach /init. Cleaning the decoded path
// before the lookup makes the answer a superset of what envd itself would route,
// so "/init/", "//init" and "/files/../init" are refused here too.
func IsInternalEnvdPath(requestPath string) bool {
	if requestPath == "" {
		return false
	}
	if requestPath[0] != '/' {
		requestPath = "/" + requestPath
	}
	_, internal := internalEnvdPaths[path.Clean(requestPath)]
	return internal
}

// Generator mints access tokens from a shared secret. A nil *Generator mints
// nothing (Enabled reports false), so a deployment without the secret wired in
// degrades to the previous unauthenticated behaviour rather than panicking.
type Generator struct {
	secret []byte
}

// New returns a Generator over secret, or nil when secret is empty. Callers
// treat a nil Generator as "secured access disabled" via Enabled.
func New(secret string) *Generator {
	if secret == "" {
		return nil
	}
	return &Generator{secret: []byte(secret)}
}

// Enabled reports whether g can mint tokens. Safe on a nil receiver.
func (g *Generator) Enabled() bool {
	return g != nil && len(g.secret) > 0
}

// EnvdToken returns the hex HMAC-SHA256 envd access token for the raw sandbox
// UUID. Empty when the generator is disabled.
func (g *Generator) EnvdToken(rawSandboxID string) string {
	if !g.Enabled() {
		return ""
	}
	return g.hash([]byte(rawSandboxID))
}

// TrafficToken returns the hex HMAC-SHA256 traffic access token for the raw
// sandbox UUID. Empty when the generator is disabled.
func (g *Generator) TrafficToken(rawSandboxID string) string {
	if !g.Enabled() {
		return ""
	}
	return g.hash([]byte(sandboxTrafficPrefix + rawSandboxID))
}

// ValidateTraffic reports whether provided matches the traffic token for the raw
// sandbox UUID, using a constant-time comparison. False when the generator is
// disabled or provided is empty.
func (g *Generator) ValidateTraffic(rawSandboxID, provided string) bool {
	if !g.Enabled() || provided == "" {
		return false
	}
	expected := g.TrafficToken(rawSandboxID)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (g *Generator) hash(content []byte) string {
	mac := hmac.New(sha256.New, g.secret)
	_, _ = mac.Write(content) // hash.Hash.Write never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}
