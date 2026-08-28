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

// Package wsdial wraps the gorilla WebSocket handshake so a failure says what
// the peer actually answered.
package wsdial

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// bodySnippetLimit caps how much of a rejection body is quoted. Enough for a
// JSON error object; short enough that an HTML error page cannot flood the log.
const bodySnippetLimit = 256

// Dial performs the WebSocket handshake and, on failure, returns an error that
// identifies which hop rejected it.
//
// gorilla answers every non-101 response with the same bare
// `websocket: bad handshake`, so the log line reads identically whether:
//
//   - the application rejected it   — 401 `{"error":"invalid sync token"}`
//   - the route is not registered   — 404, or 503 "sync not configured"
//   - an ingress has no matching rule for this vhost/path — 404 from nginx
//   - an edge proxy refuses upgrades outright — 403 from Envoy, and the request
//     never reaches the application at all
//
// The distinguishing evidence is already in the second return value gorilla
// hands back and was being discarded: the status, the `Server` header naming
// the hop that answered, and `X-AgentBox-Server-Version`, whose presence proves
// the request reached an AgentBox backend rather than dying in front of it.
func Dial(d *websocket.Dialer, rawURL string, hdr http.Header) (*websocket.Conn, error) {
	conn, resp, err := d.Dial(rawURL, hdr)
	if err == nil {
		return conn, nil
	}
	if resp == nil {
		// Transport-level failure (DNS, TCP, TLS) — no response to describe.
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	var b strings.Builder
	fmt.Fprintf(&b, "%v (HTTP %s", err, resp.Status)
	if server := resp.Header.Get("Server"); server != "" {
		fmt.Fprintf(&b, ", server=%q", server)
	}
	if ver := resp.Header.Get("X-AgentBox-Server-Version"); ver != "" {
		fmt.Fprintf(&b, ", reached-agentbox=%s", ver)
	} else {
		b.WriteString(", did not reach AgentBox — rejected by a hop in front")
	}
	if snippet := readSnippet(resp.Body); snippet != "" {
		fmt.Fprintf(&b, ", body=%q", snippet)
	}
	b.WriteString(")")
	return nil, errors.New(b.String())
}

// readSnippet returns the first bodySnippetLimit bytes with whitespace
// collapsed, so a multi-line HTML error page stays on one log line.
func readSnippet(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, bodySnippetLimit))
	if err != nil && len(raw) == 0 {
		return ""
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}
