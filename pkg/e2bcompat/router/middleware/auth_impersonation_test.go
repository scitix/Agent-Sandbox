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

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// iamFake resolves any team/user to a namespace derived from them.
type iamFake struct{}

func (iamFake) ResolveNamespace(_ context.Context, team, user string) (string, *domain.AppError) {
	return "t-" + team + "-" + user, nil
}

func ctxWithHeaders(team, user string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/sandboxes", nil)
	if team != "" {
		c.Request.Header.Set(ImpersonateTeamHeader, team)
	}
	if user != "" {
		c.Request.Header.Set(ImpersonateUserHeader, user)
	}
	return c, rec
}

func TestApplyImpersonation_AdminIsSwitched(t *testing.T) {
	c, _ := ctxWithHeaders("ai-infra", "alice")
	auth := domain.AuthInfo{Role: apikey.RoleAdmin, User: "admin", Namespace: "default"}

	if ok := applyImpersonation(c, &auth, iamFake{}); !ok {
		t.Fatal("expected impersonation to be applied")
	}
	if auth.User != "alice" || auth.Team != "ai-infra" {
		t.Fatalf("identity not switched: %+v", auth)
	}
	if auth.Namespace != "t-ai-infra-alice" {
		t.Fatalf("namespace must come from IAM, got %q", auth.Namespace)
	}
	// The role stays admin: the credential is still the admin's, and the audit
	// trail depends on that remaining visible.
	if auth.Role != apikey.RoleAdmin {
		t.Fatalf("role must not be downgraded, got %q", auth.Role)
	}
}

// A stray header on an ordinary user's request must not break the request, and
// must not grant anything either.
func TestApplyImpersonation_NonAdminHeadersIgnored(t *testing.T) {
	c, _ := ctxWithHeaders("ai-infra", "alice")
	auth := domain.AuthInfo{Role: "user", User: "bob", Team: "team-b", Namespace: "t-team-b-bob"}

	if ok := applyImpersonation(c, &auth, iamFake{}); !ok {
		t.Fatal("a non-admin's headers should be ignored, not refused")
	}
	if auth.User != "bob" || auth.Namespace != "t-team-b-bob" {
		t.Fatalf("a non-admin must not be able to impersonate: %+v", auth)
	}
}

// One header alone is an incomplete identity; acting on half of it would pick
// an unintended namespace.
func TestApplyImpersonation_RequiresBothHeaders(t *testing.T) {
	for _, tc := range []struct{ team, user string }{{"ai-infra", ""}, {"", "alice"}, {"", ""}} {
		c, _ := ctxWithHeaders(tc.team, tc.user)
		auth := domain.AuthInfo{Role: apikey.RoleAdmin, User: "admin", Namespace: "default"}
		if ok := applyImpersonation(c, &auth, iamFake{}); !ok {
			t.Fatalf("team=%q user=%q: should be a no-op, not a refusal", tc.team, tc.user)
		}
		if auth.User != "admin" || auth.Namespace != "default" {
			t.Fatalf("team=%q user=%q: identity must be untouched: %+v", tc.team, tc.user, auth)
		}
	}
}

// Without IAM the namespace cannot be resolved, and guessing it would address
// the wrong tenant's data.
// No resolver is no longer a refusal — see
// TestImpersonationWorksWithoutANamespaceResolver below, which replaced this
// test's inverse assertion. The namespace was the only thing that needed one,
// and the deployment that has no resolver is also the one that reads no
// namespace.

// A deployment with no namespaces to resolve must still be able to say WHO is
// acting.
//
// The manager cluster runs this middleware with no IAM service, because it has
// no tenant namespaces and nothing it serves reads one. Refusing the request
// for want of a namespace made signing in through IAM as an admin break the
// assistant outright: every conversation opens by asking the hub for a key, and
// the browser names the acting identity on every such call — including when
// that identity is the caller themselves, which is the ordinary case rather
// than the exotic one.
func TestImpersonationWorksWithoutANamespaceResolver(t *testing.T) {
	c, rec := ctxWithHeaders("acme", "bob")
	auth := domain.AuthInfo{Role: apikey.RoleAdmin, Team: "ops", User: "root"}

	if ok := applyImpersonation(c, &auth, nil); !ok {
		t.Fatal("refused the request because it could not resolve a namespace")
	}
	if auth.Team != "acme" || auth.User != "bob" {
		t.Fatalf("identity was not applied: %+v", auth)
	}
	// Nothing written: the call carries on rather than answering an error.
	if rec.Code != http.StatusOK {
		t.Fatalf("an error response was written: %d", rec.Code)
	}
}

// The relaxation above is about a namespace lookup, not about who may
// impersonate. That rule is the only thing here that authorises anything, so it
// holds whether or not a resolver exists.
func TestOnlyAnAdminMayImpersonateEvenWithoutAResolver(t *testing.T) {
	c, _ := ctxWithHeaders("acme", "bob")
	auth := domain.AuthInfo{Role: apikey.RoleTenant, Team: "ops", User: "root"}

	if ok := applyImpersonation(c, &auth, nil); !ok {
		t.Fatal("a tenant's headers should be ignored, not refused")
	}
	if auth.Team != "ops" || auth.User != "root" {
		t.Fatalf("a tenant impersonated someone: %+v", auth)
	}
}

// With a resolver present nothing changes — the worker always has one, and its
// behaviour must be exactly what it was.
func TestAResolverIsStillUsedWhenThereIsOne(t *testing.T) {
	c, _ := ctxWithHeaders("acme", "bob")
	auth := domain.AuthInfo{Role: apikey.RoleAdmin, Namespace: "default"}

	if ok := applyImpersonation(c, &auth, iamFake{}); !ok {
		t.Fatal("refused with a resolver present")
	}
	if auth.Namespace != "t-acme-bob" {
		t.Fatalf("namespace not resolved: %q", auth.Namespace)
	}
}
