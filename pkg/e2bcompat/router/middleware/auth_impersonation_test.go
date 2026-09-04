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
func TestApplyImpersonation_NoIAMIsRefused(t *testing.T) {
	c, rec := ctxWithHeaders("ai-infra", "alice")
	auth := domain.AuthInfo{Role: apikey.RoleAdmin, User: "admin", Namespace: "default"}

	if ok := applyImpersonation(c, &auth, nil); ok {
		t.Fatal("expected a refusal when IAM is unavailable")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
