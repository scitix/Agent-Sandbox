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

package quota

import (
	"context"
	"strings"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

const NoopName = "noop"

// Noop is a Provider that reports the quota feature as disabled. It is the
// default for open-source builds where no quota backend is configured.
type Noop struct{}

// NewNoop returns a disabled quota Provider. Safe to share.
func NewNoop() Provider { return Noop{} }

// NoopFactory is the Factory form of NewNoop — no Handle or Args are used.
// Registered at package init() so callers can Build("noop", h, nil) without
// any extra wiring.
func NoopFactory(_ framework.Handle, _ framework.Args) (Provider, error) {
	return NewNoop(), nil
}

func init() { Register(NoopName, NoopFactory) }

func (Noop) Enabled() bool { return false }

func (Noop) ListForUser(_ context.Context, _, _ string) ([]gen.Quota, *domain.AppError) {
	return nil, nil
}

func (Noop) DeriveShortName(quotaURL string) string {
	return DeriveDefaultShortName(quotaURL)
}

// DeriveDefaultShortName is the open-source short-name extractor.
//
// The convention for QuotaURLs in this project is `{tenant/lab}.{pool}.{type}`
// (e.g. "zxli.ai-lab.math.exclusive", "upgrader.autoupg.test.ondemand").
// The most useful identifier for naming a member SandboxPool is the trailing
// segment — it identifies the resource class (ondemand / spot / exclusive /
// preempt etc.) that the Pool draws from.
//
// Algorithm:
//  1. Strip any URL scheme + host prefix (we accept either "scheme://host/x.y.z"
//     or bare "x.y.z" forms).
//  2. Split the remainder on '.', return the last segment.
//  3. Sanitise to RFC 1123 label characters: lower-case, keep [a-z0-9-],
//     collapse runs of '-' and trim leading/trailing '-'.
//  4. Empty result → return "" so the caller falls back to a hash-based suffix.
//
// Closed-source providers (e.g. Scitix) can override this with a richer
// catalog lookup; they typically delegate here for unknown URLs.
func DeriveDefaultShortName(quotaURL string) string {
	if quotaURL == "" {
		return ""
	}
	raw := quotaURL
	// Strip scheme + host (best effort — empty when no "://" present).
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
		if slash := strings.Index(raw, "/"); slash >= 0 {
			raw = raw[slash+1:]
		}
	}
	// Drop query / fragment, then return the trailing dot-segment.
	for _, sep := range []string{"?", "#"} {
		if i := strings.Index(raw, sep); i >= 0 {
			raw = raw[:i]
		}
	}
	last := raw
	if i := strings.LastIndex(raw, "."); i >= 0 {
		last = raw[i+1:]
	}
	return sanitiseLabel(last)
}

// sanitiseLabel lower-cases and reduces s to RFC 1123 label characters.
// Non-alphanumeric runs become a single '-'; leading/trailing '-' is
// trimmed. An empty / unrecoverable input returns "".
func sanitiseLabel(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	dash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || r == '.' || r == ' ':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := b.String()
	out = strings.TrimRight(out, "-")
	return out
}
