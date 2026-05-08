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

package extproc

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

// authenticate verifies the API key from the header map and returns the
// namespace associated with the key.
func (s *Server) authenticate(ctx context.Context, hdrMap map[string]string) (string, error) {
	const defaultNamespace = "default"
	if s.adminKeyMgr == nil {
		// Auth disabled – return the default namespace.
		return defaultNamespace, nil
	}

	providedKey := strings.TrimSpace(hdrMap[apiKeyHeader])
	if providedKey == "" {
		return "", fmt.Errorf("missing api key")
	}

	// Constant-time comparison against the admin key.
	if s.adminKeyMgr.IsAdminKey(providedKey) {
		return defaultNamespace, nil
	}

	// Fall back to KeyStore tenant validation.
	if s.keyStore == nil {
		return "", fmt.Errorf("invalid api key")
	}
	meta, err := s.keyStore.Validate(ctx, providedKey)
	if err != nil {
		return "", fmt.Errorf("invalid api key: %w", err)
	}

	ns := meta.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return ns, nil
}

// RouteTarget holds the extracted sandbox ID, port, and the rewritten path.
type RouteTarget struct {
	SandboxID     string
	Port          int
	RewrittenPath string
}

// extractTarget parses headers or URL to obtain SandboxID, Port, and the rewritten path.
// Rules:
//  1. If X-Sandbox-ID header is present, X-Sandbox-Port must also be present and valid.
//     The rewritten path is set to the original :path (no change).
//  2. Otherwise, the URL path must contain the marker "/sandboxes/". After the marker,
//     the first segment is the sandbox ID, and the second segment must be a valid port number.
//     The remaining path (after these two segments) becomes the rewritten path, preserving query/fragment.
func extractTarget(hdrMap map[string]string) (RouteTarget, error) {
	target := RouteTarget{}

	// Strategy 1: Explicit headers
	if id := strings.TrimSpace(hdrMap[sandboxIDHeader]); id != "" {
		portStr := strings.TrimSpace(hdrMap[sandboxPortHeader])
		if portStr == "" {
			return target, fmt.Errorf("missing %s header when %s is set", sandboxPortHeader, sandboxIDHeader)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return target, fmt.Errorf("invalid port in %s header", sandboxPortHeader)
		}
		target.SandboxID = id
		target.Port = port
		// Preserve original path
		target.RewrittenPath = hdrMap[":path"]
		return target, nil
	}

	// Strategy 2: URL path parsing using /sandboxes/ marker
	path := hdrMap[":path"]
	if path == "" {
		return target, nil
	}

	// Locate the marker "/sandboxes/"
	_, after, ok := strings.Cut(path, pathSandboxMarker)
	if !ok {
		// No marker found, cannot extract sandbox ID
		return target, nil
	}
	rest := after // e.g. "my-sbx/8080/api/run?foo=bar"

	// Split off query/fragment
	basePath := rest
	suffix := ""
	if q := strings.IndexAny(rest, "?#"); q != -1 {
		basePath = rest[:q]
		suffix = rest[q:]
	}

	parts := strings.Split(strings.TrimPrefix(basePath, "/"), "/")
	if len(parts) < 2 {
		return target, fmt.Errorf("URL path after %s must contain at least sandbox ID and port", pathSandboxMarker)
	}

	target.SandboxID = parts[0]

	// Second part must be a valid port number
	port, err := strconv.Atoi(parts[1])
	if err != nil || port <= 0 || port > 65535 {
		return target, fmt.Errorf("invalid port number %q in URL path", parts[1])
	}
	target.Port = port

	// Reconstruct the rewritten path (skip sandbox ID and port)
	remainingPath := "/" + strings.Join(parts[2:], "/")
	if remainingPath == "/" && len(parts[2:]) == 0 && !strings.HasSuffix(basePath, "/") {
		// No extra path segments, just "/"
		remainingPath = "/"
	}
	target.RewrittenPath = remainingPath + suffix

	return target, nil
}

// extractHeaders builds a lower-case map from Envoy's HttpHeaders message.
// For binary header values (RawValue), the bytes are interpreted as UTF-8.
func extractHeaders(httpHeaders *extProcPb.HttpHeaders) map[string]string {
	hdrMap := make(map[string]string)
	if httpHeaders == nil {
		return hdrMap
	}
	for _, hv := range httpHeaders.GetHeaders().GetHeaders() {
		key := strings.ToLower(hv.GetKey())
		// Prefer RawValue (binary-safe); fall back to Value.
		val := string(hv.GetRawValue())
		if val == "" {
			val = hv.GetValue()
		}
		hdrMap[key] = val
	}
	return hdrMap
}

// immediateError builds a ProcessingResponse that instructs Envoy to reply
// with an HTTP error immediately, without forwarding to the upstream.
func immediateError(code typev3.StatusCode, message string) *extProcPb.ProcessingResponse {
	body := fmt.Sprintf(`{"error":%q}`, message)
	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extProcPb.ImmediateResponse{
				Status: &typev3.HttpStatus{Code: code},
				Body:   []byte(body),
				Headers: &extProcPb.HeaderMutation{
					SetHeaders: []*corev3.HeaderValueOption{
						{
							Header: &corev3.HeaderValue{
								Key:   "content-type",
								Value: "application/json",
							},
						},
					},
				},
			},
		},
	}
}
