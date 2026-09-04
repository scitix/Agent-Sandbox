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
	"encoding/base64"
	"maps"
	"sort"
	"strconv"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
)

// The E2B /secrets endpoints. Values are write-only throughout: accepted by
// create and update, returned by nothing. The read shape the service layer
// hands back has no value field at all, so that is a property of the types
// rather than a rule to remember.
//
// Every operation is scoped to (namespace, user) derived from the API key, so
// the isolation needs no new mechanism: whichever key called decides whose
// vault is addressed.

// vaultDefaultPageLimit matches the upstream default page size.
const vaultDefaultPageLimit = 100

// vaultUnavailable is returned when the deployment has no vault wired. Callers
// see the same 501 they saw before the vault existed, rather than a confusing
// empty list.
func (s *Server) vaultUnavailable(op string) unsupported {
	return unsupportedOp(op, catUnimplemented, msgSecrets)
}

// vaultItemToGen projects one entry onto the E2B wire shape.
func vaultItemToGen(item service.VaultItem) e2bgen.Secret {
	md := e2bgen.SecretMetadata{}
	maps.Copy(md, item.Metadata)
	return e2bgen.Secret{
		SecretID:       item.SecretID(),
		Name:           item.Name,
		CurrentVersion: item.Version,
		Metadata:       md,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

// vaultErrorResponse maps a service error onto the status the caller should see.
func vaultStatus(appErr *apidomain.AppError) int {
	switch appErr.Code {
	case apidomain.ErrCodeBadRequest, apidomain.ErrCodeNotFound, apidomain.ErrCodeConflict,
		apidomain.ErrCodeForbidden, apidomain.ErrCodeUnauthorized:
		return int(appErr.Code)
	default:
		return 500
	}
}

func (s *Server) GetSecrets(ctx context.Context, req e2bgen.GetSecretsRequestObject) (e2bgen.GetSecretsResponseObject, error) {
	if s.vault == nil {
		return s.vaultUnavailable("GetSecrets"), nil
	}
	auth := authFrom(ctx)
	items, appErr := s.vault.List(ctx, auth.Namespace, auth.User)
	if appErr != nil {
		pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "list", "error").Inc()
		return statusJSON{status: vaultStatus(appErr), msg: appErr.Message}, nil
	}
	pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "list", "success").Inc()

	// Entries are already name-sorted, which is what makes an offset cursor
	// stable enough to page through.
	offset := min(decodeNextToken(req.Params.NextToken), len(items))
	limit := vaultDefaultPageLimit
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = int(*req.Params.Limit)
	}
	end := min(offset+limit, len(items))

	page := make([]e2bgen.Secret, 0, end-offset)
	for _, item := range items[offset:end] {
		page = append(page, vaultItemToGen(item))
	}
	next := ""
	if end < len(items) {
		next = encodeNextToken(end)
	}
	return e2bgen.GetSecrets200JSONResponse{
		Body:    page,
		Headers: e2bgen.GetSecrets200ResponseHeaders{XNextToken: next},
	}, nil
}

func (s *Server) GetSecretsSecretID(ctx context.Context, req e2bgen.GetSecretsSecretIDRequestObject) (e2bgen.GetSecretsSecretIDResponseObject, error) {
	if s.vault == nil {
		return s.vaultUnavailable("GetSecretsSecretID"), nil
	}
	auth := authFrom(ctx)
	item, appErr := s.vault.Get(ctx, auth.Namespace, auth.User, req.SecretID)
	if appErr != nil {
		pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "get", "error").Inc()
		return statusJSON{status: vaultStatus(appErr), msg: appErr.Message}, nil
	}
	pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "get", "success").Inc()
	return e2bgen.GetSecretsSecretID200JSONResponse(vaultItemToGen(*item)), nil
}

func (s *Server) PostSecrets(ctx context.Context, req e2bgen.PostSecretsRequestObject) (e2bgen.PostSecretsResponseObject, error) {
	if s.vault == nil {
		return s.vaultUnavailable("PostSecrets"), nil
	}
	if req.Body == nil {
		return statusJSON{status: 400, msg: "request body required: name and value"}, nil
	}
	auth := authFrom(ctx)
	item, appErr := s.vault.Create(ctx, auth.Namespace, auth.User, service.VaultCreateInput{
		Name:     req.Body.Name,
		Value:    req.Body.Value,
		Metadata: metadataFromGen(req.Body.Metadata),
	})
	if appErr != nil {
		pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "create", "error").Inc()
		return statusJSON{status: vaultStatus(appErr), msg: appErr.Message}, nil
	}
	pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "create", "success").Inc()
	return e2bgen.PostSecrets201JSONResponse(vaultItemToGen(*item)), nil
}

func (s *Server) PostSecretsSecretID(ctx context.Context, req e2bgen.PostSecretsSecretIDRequestObject) (e2bgen.PostSecretsSecretIDResponseObject, error) {
	if s.vault == nil {
		return s.vaultUnavailable("PostSecretsSecretID"), nil
	}
	if req.Body == nil {
		return statusJSON{status: 400, msg: "request body required: value"}, nil
	}
	auth := authFrom(ctx)
	item, appErr := s.vault.Update(ctx, auth.Namespace, auth.User, req.SecretID, service.VaultUpdateInput{
		Value:       req.Body.Value,
		Metadata:    metadataFromGen(req.Body.Metadata),
		MetadataSet: req.Body.Metadata != nil,
	})
	if appErr != nil {
		pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "update", "error").Inc()
		return statusJSON{status: vaultStatus(appErr), msg: appErr.Message}, nil
	}
	pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "update", "success").Inc()
	return e2bgen.PostSecretsSecretID200JSONResponse(vaultItemToGen(*item)), nil
}

func (s *Server) DeleteSecretsSecretID(ctx context.Context, req e2bgen.DeleteSecretsSecretIDRequestObject) (e2bgen.DeleteSecretsSecretIDResponseObject, error) {
	if s.vault == nil {
		return s.vaultUnavailable("DeleteSecretsSecretID"), nil
	}
	auth := authFrom(ctx)
	if appErr := s.vault.Delete(ctx, auth.Namespace, auth.User, req.SecretID); appErr != nil {
		pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "delete", "error").Inc()
		return statusJSON{status: vaultStatus(appErr), msg: appErr.Message}, nil
	}
	pkgmetrics.VaultOperationTotal.WithLabelValues(auth.Namespace, "delete", "success").Inc()
	return e2bgen.DeleteSecretsSecretID204Response{}, nil
}

// metadataFromGen converts the wire metadata map, treating absent as nil.
func metadataFromGen(md *e2bgen.SecretMetadata) map[string]string {
	if md == nil {
		return nil
	}
	out := make(map[string]string, len(*md))
	maps.Copy(out, *md)
	return out
}

// sortedNames is used by tests and by the rule parser to report names in a
// stable order.
func sortedNames(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var (
	_ = base64.RawURLEncoding
	_ = strconv.Itoa
)
