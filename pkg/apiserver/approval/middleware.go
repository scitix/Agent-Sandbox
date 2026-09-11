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
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

const (
	// SessionHeader names the conversation, shell session or run this call
	// belongs to. It is the unit "approve for this session" is about, and it is
	// the caller's to declare: the platform cannot infer it, because the same
	// key legitimately serves many sessions at once.
	//
	// A caller that omits it is not refused — it simply cannot be granted
	// anything wider than one call, which is the safe direction to fail in.
	SessionHeader = "X-AgentBox-Session-Id"

	// BizErrApprovalRequired tells a client that the call is well-formed and
	// permitted, and is waiting on a person. It is a distinct code from any
	// permission error because the remedy is different: not "get access", but
	// "go and click this".
	BizErrApprovalRequired domain.BusinessErrorCode = "APPROVAL_REQUIRED"

	// BizErrForbiddenForAgent tells a client the call will never be permitted
	// for this credential and there is nothing to wait for.
	//
	// Distinct from APPROVAL_REQUIRED because the remedies are opposite: one
	// says "a person is about to decide, ask again shortly", the other says
	// "stop asking, and hand this to a person". An agent that cannot tell them
	// apart polls forever for an approval nobody will ever be shown.
	BizErrForbiddenForAgent domain.BusinessErrorCode = "FORBIDDEN_FOR_AGENT"

	// StatusApprovalRequired is 428 Precondition Required.
	//
	// Not 403: a client cannot tell a real permission failure from a pending
	// one, and every SDK in front of us maps 403 to a terminal error. Not 202
	// either — the Python SDK treats anything under 400 as success, so a 202
	// would read as "created" and the caller would carry on with nothing.
	StatusApprovalRequired = http.StatusPreconditionRequired

	// Requests larger than this are fingerprinted by route alone. A body that
	// big is not something a person is reading off an approval card anyway, and
	// buffering it to hash it would be the gate's own denial-of-service.
	maxFingerprintBody = 1 << 20
)

// Detail is the structured half of a 428. It is what the CLI renders as its
// `approval:` trailer and what the console deep-links from.
type Detail struct {
	ApprovalID string `json:"approvalId"`
	Operation  string `json:"operation"`
	Summary    string `json:"summary"`
	// OnceOnly is here so a client can say "this one cannot be approved for the
	// session" without having to know the catalogue.
	OnceOnly  bool   `json:"onceOnly"`
	URL       string `json:"url,omitempty"`
	PollURL   string `json:"pollUrl"`
	ExpiresAt string `json:"expiresAt"`
}

// Identity is what the middleware needs to know about the caller. It is a
// function rather than a direct read of the auth context so the two API
// surfaces — which populate that context slightly differently — can each say
// what a gated caller looks like without this package importing either.
type Identity struct {
	// Gated is false for anyone who should never be asked: console sessions,
	// admin keys, and every credential not marked as acting unattended.
	Gated     bool
	Principal Principal
	// KeyApprovals are the operations this credential is already allowed to
	// perform, as recorded on the credential itself.
	//
	// Read from the caller's own metadata rather than from the gate's memory so
	// that a permission survives a restart of this process: the memory is a
	// cache of decisions made since it started, and after a rollout it is
	// simply empty.
	KeyApprovals []string
}

// IdentityFunc extracts the caller from a request.
type IdentityFunc func(c *gin.Context) Identity

// ConsoleURLFunc builds the page a person opens to decide. Empty when the
// deployment has no console configured — the CLI still works from `pollUrl`,
// and a link to nowhere is worse than no link.
type ConsoleURLFunc func(approvalID string) string

// PageURLFunc builds a link to a console PAGE rather than to one approval.
//
// A forbidden operation has no approval to link to — the whole answer is "do
// this yourself, over there" — so the refusal needs an address for the page
// that does it. May be nil, and may return "" for the same reason as
// ConsoleURLFunc: the console's address arrives after this server is serving.
type PageURLFunc func(page string) string

// New returns the gate.
//
// It must run AFTER authentication (it needs the principal) and after routing
// (it needs the route template, not the concrete URL). Both hold when it is
// appended to the generated router's middleware slice, which is where it goes.
func New(store *Store, identity IdentityFunc, consoleURL ConsoleURLFunc, pageURL PageURLFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsWrite(c.Request.Method) {
			c.Next()
			return
		}
		op, gatedRoute := Lookup(c.Request.Method, c.FullPath())
		if !gatedRoute {
			c.Next()
			return
		}
		id := identity(c)
		if !id.Gated {
			c.Next()
			return
		}

		if op.Forbidden {
			// No approval is created. What is being asked for is the ability to
			// stop being gated, and an approval card reading "create an API key"
			// does not say that to the person clicking it. The link is the whole
			// answer: they do it as themselves.
			detail := Detail{Operation: op.ID, Summary: op.Summary}
			if pageURL != nil {
				detail.URL = pageURL(forbiddenPage(op.ID))
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "this credential may not " + strings.ToLower(op.Summary) +
					": issuing a credential would let it act without the approval gate. " +
					"Open the console and do it as yourself.",
				"errorCode": string(BizErrForbiddenForAgent),
				"detail":    detail,
			})
			return
		}

		body := readBodyForFingerprint(c)
		fp := Fingerprint(c.Request.Method, c.FullPath(), body)
		session := c.GetHeader(SessionHeader)

		if store.Allow(id.Principal, id.KeyApprovals, session, op.ID, fp) {
			c.Next()
			return
		}

		req := store.Challenge(Request{
			Principal:   id.Principal,
			SessionID:   session,
			Operation:   op.ID,
			OnceOnly:    op.OnceOnly,
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			Summary:     summarise(op, c),
			Fingerprint: fp,
		})

		detail := Detail{
			ApprovalID: req.ID,
			Operation:  req.Operation,
			Summary:    req.Summary,
			OnceOnly:   req.OnceOnly,
			PollURL:    "/v1/approvals/" + req.ID,
			ExpiresAt:  req.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		// Empty is a normal answer, not a failure: the console address arrives
		// from the hub after this server is already serving, so early refusals
		// legitimately carry no link. The CLI polls `pollUrl` either way.
		if consoleURL != nil {
			detail.URL = consoleURL(req.ID)
		}

		bizCode := string(BizErrApprovalRequired)
		c.AbortWithStatusJSON(StatusApprovalRequired, gin.H{
			"error":     "approval required: " + req.Summary,
			"errorCode": bizCode,
			"detail":    detail,
		})
	}
}

// forbiddenPage names the console page that performs a forbidden operation.
//
// Keyed on the op id rather than the route, because the id is already the unit
// a person reasons about, and the page is a property of the act rather than of
// the URL that attempted it.
func forbiddenPage(opID string) string {
	switch opID {
	case "apikey.create":
		return "api-keys"
	default:
		return ""
	}
}

// readBodyForFingerprint consumes the body and puts it back.
//
// The handler downstream has not read it yet and must still be able to; gin
// gives no replay of its own, so the body is buffered here and the reader
// replaced. Anything over the cap is hashed as an empty body — see
// maxFingerprintBody.
func readBodyForFingerprint(c *gin.Context) []byte {
	if c.Request.Body == nil {
		return nil
	}
	limited := io.LimitReader(c.Request.Body, maxFingerprintBody+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil
	}
	if len(buf) > maxFingerprintBody {
		// Put back what was read followed by the rest of the stream, so the
		// handler still sees a whole body even though we decline to hash it.
		c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), c.Request.Body))
		return nil
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(buf))
	return buf
}

// summarise names the thing being acted on.
//
// "Delete an environment" is not a question anyone can answer; "Delete an
// environment (foo)" is. The route parameters are the only naming material
// available at this layer, and they are exactly the identifying part of the
// URL.
func summarise(op Op, c *gin.Context) string {
	var names []string
	for _, p := range c.Params {
		if p.Value != "" {
			names = append(names, p.Value)
		}
	}
	if len(names) == 0 {
		return op.Summary
	}
	return op.Summary + " (" + strings.Join(names, "/") + ")"
}
