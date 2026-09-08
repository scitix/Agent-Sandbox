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
	"context"

	"github.com/scitix/agent-sandbox/pkg/apiserver/approval"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
)

// The four routes that answer the gate. Everything else in this API can only be
// refused by it; these are where the refusal gets resolved.
//
// Read routes are open to the credential itself — an agent polling for its own
// answer is the normal case, and it learns nothing it did not already know.
// DECIDING is not: see the console-only check below.

func principalOf(auth domain.AuthInfo) approval.Principal {
	return approval.Principal{Team: auth.Team, User: auth.User, KeyID: auth.KeyID}
}

// requireHuman refuses a decision made by anything other than a console
// session.
//
// Without it the gate is a formality: an agent refused a write would hold, in
// its own hands, the credential needed to approve it. The check is on the
// authentication METHOD rather than on a role, because the property that
// matters is "a person was present", and a JWT is the only thing this API
// issues that means that.
func requireHuman(auth domain.AuthInfo) *domain.AppError {
	if auth.AuthMethod == "jwt" {
		return nil
	}
	return domain.NewForbidden(
		"approvals can only be decided from the console: the credential that " +
			"made the request cannot approve it")
}

func approvalPrincipalToGen(p approval.Principal) gen.ApprovalPrincipal {
	out := gen.ApprovalPrincipal{Team: p.Team, User: p.User}
	if p.KeyID != "" {
		out.KeyId = &p.KeyID
	}
	return out
}

func approvalRequestToGen(r approval.Request) gen.ApprovalRequest {
	out := gen.ApprovalRequest{
		Id:        r.ID,
		Principal: approvalPrincipalToGen(r.Principal),
		Operation: r.Operation,
		OnceOnly:  r.OnceOnly,
		Method:    r.Method,
		Path:      r.Path,
		Summary:   r.Summary,
		CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		Status:    gen.ApprovalRequestStatus(r.Status),
	}
	if r.SessionID != "" {
		out.SessionId = &r.SessionID
	}
	if r.Scope != "" {
		scope := gen.ApprovalRequestScope(r.Scope)
		out.Scope = &scope
	}
	if r.DecidedBy != "" {
		out.DecidedBy = &r.DecidedBy
	}
	if !r.DecidedAt.IsZero() {
		t := r.DecidedAt
		out.DecidedAt = &t
	}
	return out
}

func approvalGrantToGen(g approval.Grant) gen.ApprovalGrant {
	out := gen.ApprovalGrant{
		Id:        g.ID,
		Principal: approvalPrincipalToGen(g.Principal),
		Scope:     gen.ApprovalGrantScope(g.Scope),
		Operation: g.Operation,
		CreatedAt: g.CreatedAt,
		GrantedBy: g.GrantedBy,
	}
	if g.SessionID != "" {
		out.SessionId = &g.SessionID
	}
	if !g.ExpiresAt.IsZero() {
		t := g.ExpiresAt
		out.ExpiresAt = &t
	}
	return out
}

// ListApprovals returns what is waiting on this person and what they have
// already allowed.
func (s *Server) ListApprovals(
	ctx context.Context,
	_ gen.ListApprovalsRequestObject,
) (gen.ListApprovalsResponseObject, error) {
	auth := httpctx.AuthFrom(ctx)
	if s.approvals == nil {
		// An empty board rather than an error: a deployment with no gate has
		// nothing pending, which is exactly what the console should draw.
		return gen.ListApprovals200JSONResponse{
			Pending: []gen.ApprovalRequest{},
			Grants:  []gen.ApprovalGrant{},
		}, nil
	}
	pending, grants := s.approvals.List(principalOf(auth))
	out := gen.ListApprovals200JSONResponse{
		Pending: make([]gen.ApprovalRequest, 0, len(pending)),
		Grants:  make([]gen.ApprovalGrant, 0, len(grants)),
	}
	for _, r := range pending {
		out.Pending = append(out.Pending, approvalRequestToGen(r))
	}
	for _, g := range grants {
		out.Grants = append(out.Grants, approvalGrantToGen(g))
	}
	return out, nil
}

// GetApproval is what a blocked client polls.
func (s *Server) GetApproval(
	ctx context.Context,
	request gen.GetApprovalRequestObject,
) (gen.GetApprovalResponseObject, error) {
	auth := httpctx.AuthFrom(ctx)
	if s.approvals == nil {
		return gen.GetApproval404JSONResponse(
			errResp(ctx, domain.NewNotFound("approval request not found"))), nil
	}
	r, ok := s.approvals.Get(request.ApprovalId)
	// Someone else's request is reported as absent rather than as forbidden:
	// telling a caller that an id exists but is not theirs is a fact about
	// another person's activity.
	if !ok || !samePerson(r.Principal, auth) {
		return gen.GetApproval404JSONResponse(
			errResp(ctx, domain.NewNotFound("approval request not found"))), nil
	}
	return gen.GetApproval200JSONResponse(approvalRequestToGen(r)), nil
}

// DecideApproval answers one pending request.
func (s *Server) DecideApproval(
	ctx context.Context,
	request gen.DecideApprovalRequestObject,
) (gen.DecideApprovalResponseObject, error) {
	auth := httpctx.AuthFrom(ctx)
	if appErr := requireHuman(auth); appErr != nil {
		return gen.DecideApproval403JSONResponse(errResp(ctx, appErr)), nil
	}
	if s.approvals == nil || request.Body == nil {
		return gen.DecideApproval404JSONResponse(
			errResp(ctx, domain.NewNotFound("approval request not found"))), nil
	}

	existing, ok := s.approvals.Get(request.ApprovalId)
	if !ok || !samePerson(existing.Principal, auth) {
		return gen.DecideApproval404JSONResponse(
			errResp(ctx, domain.NewNotFound("approval request not found"))), nil
	}

	approve := request.Body.Decision == gen.Approve
	// Absent scope means the narrowest one. Defaulting the other way would let
	// a client that forgot the field hand out a standing permission.
	scope := approval.ScopeOnce
	if request.Body.Scope != nil {
		scope = approval.Scope(*request.Body.Scope)
	}

	decided, err := s.approvals.Decide(request.ApprovalId, approve, scope, decidedBy(auth))
	if err != nil {
		return gen.DecideApproval400JSONResponse(
			errResp(ctx, domain.NewBadRequest(err.Error()))), nil
	}
	return gen.DecideApproval200JSONResponse(approvalRequestToGen(decided)), nil
}

// RevokeApprovalGrant withdraws a standing permission.
func (s *Server) RevokeApprovalGrant(
	ctx context.Context,
	request gen.RevokeApprovalGrantRequestObject,
) (gen.RevokeApprovalGrantResponseObject, error) {
	auth := httpctx.AuthFrom(ctx)
	if appErr := requireHuman(auth); appErr != nil {
		// A 404 rather than the 403 the decision route gives: this route's spec
		// declares no 403, and revoking is idempotent enough that "there is no
		// such grant for you" is true from where the caller stands.
		return gen.RevokeApprovalGrant404JSONResponse(
			errResp(ctx, domain.NewNotFound("grant not found"))), nil
	}
	if s.approvals == nil || !s.approvals.Revoke(principalOf(auth), request.GrantId) {
		return gen.RevokeApprovalGrant404JSONResponse(
			errResp(ctx, domain.NewNotFound("grant not found"))), nil
	}
	return gen.RevokeApprovalGrant204Response{}, nil
}

// samePerson ignores the key: a person decides for every credential they hold,
// and the console session doing the deciding carries no key at all.
func samePerson(p approval.Principal, auth domain.AuthInfo) bool {
	return p.Team == auth.Team && p.User == auth.User
}

func decidedBy(auth domain.AuthInfo) string {
	if auth.Email != "" {
		return auth.Email
	}
	return auth.Team + "/" + auth.User
}
