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

package syncmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	nativegen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
)

// templateServer implements wsproxygen.StrictServerInterface for the five
// SandboxTemplate endpoints. Auth context is read from the gin request via
// httpctx.AuthFrom — populated by jwtOrManagerTokenMiddleware.
type templateServer struct {
	m *SyncManager
}

func (s *templateServer) requireAdmin(ctx context.Context) bool {
	return httpctx.AuthFrom(ctx).Role == apikey.RoleAdmin
}

// ── ListSandboxTemplates ─────────────────────────────────────────────────────

func (s *templateServer) ListSandboxTemplates(
	ctx context.Context,
	_ wsproxygen.ListSandboxTemplatesRequestObject,
) (wsproxygen.ListSandboxTemplatesResponseObject, error) {
	if s.m.deps.TemplateService == nil {
		return wsproxygen.ListSandboxTemplates503JSONResponse{
			Error: "template sync not configured",
		}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	items, appErr := s.m.deps.TemplateService.List(ctx, auth, isAdmin)
	if appErr != nil {
		log.Printf("syncManager: template list error: %v", appErr)
		return wsproxygen.ListSandboxTemplates503JSONResponse{Error: appErr.Message}, nil
	}

	summaries := make([]nativegen.SandboxTemplateSummary, len(items))
	for i := range items {
		summaries[i] = service.TemplateToSummaryGen(&items[i])
	}
	total := len(summaries)
	return wsproxygen.ListSandboxTemplates200JSONResponse{
		Items:  summaries,
		Total:  total,
		Limit:  0,
		Offset: 0,
	}, nil
}

// ── GetSandboxTemplate ───────────────────────────────────────────────────────

func (s *templateServer) GetSandboxTemplate(
	ctx context.Context,
	request wsproxygen.GetSandboxTemplateRequestObject,
) (wsproxygen.GetSandboxTemplateResponseObject, error) {
	if s.m.deps.TemplateService == nil {
		return wsproxygen.GetSandboxTemplate503JSONResponse{
			Error: "template sync not configured",
		}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	tmpl, appErr := s.m.deps.TemplateService.Get(ctx, request.Name, auth, isAdmin)
	if appErr != nil {
		if appErrStatus(appErr) == 404 {
			return wsproxygen.GetSandboxTemplate404JSONResponse{Error: appErr.Message}, nil
		}
		return wsproxygen.GetSandboxTemplate503JSONResponse{Error: appErr.Message}, nil
	}

	genTmpl := service.TemplateToGen(tmpl)
	return wsproxygen.GetSandboxTemplate200JSONResponse{Template: genTmpl}, nil
}

// ── AdminCreateSandboxTemplate ───────────────────────────────────────────────

func (s *templateServer) AdminCreateSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminCreateSandboxTemplateRequestObject,
) (wsproxygen.AdminCreateSandboxTemplateResponseObject, error) {
	if s.m.deps.TemplateService == nil {
		return wsproxygen.AdminCreateSandboxTemplate503JSONResponse{
			Error: "template sync not configured",
		}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminCreateSandboxTemplate403JSONResponse{
			Error: "admin access required",
		}, nil
	}

	crdJSON := request.Body.CrdJson
	tmpl, parseErr := parseCRDJSONToK8s(crdJSON)
	if parseErr != nil {
		return wsproxygen.AdminCreateSandboxTemplate400JSONResponse{
			Error: parseErr.Error(),
		}, nil
	}
	injectGlobalLabel(tmpl)

	result, appErr := s.m.deps.TemplateService.Create(ctx, tmpl)
	if appErr != nil {
		log.Printf("syncManager: template create error: %v", appErr)
		switch appErrStatus(appErr) {
		case 400:
			return wsproxygen.AdminCreateSandboxTemplate400JSONResponse{Error: appErr.Message}, nil
		case 409:
			return wsproxygen.AdminCreateSandboxTemplate409JSONResponse{Error: appErr.Message}, nil
		default:
			return wsproxygen.AdminCreateSandboxTemplate503JSONResponse{Error: appErr.Message}, nil
		}
	}

	s.broadcastDomainTemplate(result)
	genTmpl := service.TemplateToGen(result)
	return wsproxygen.AdminCreateSandboxTemplate201JSONResponse{Template: genTmpl}, nil
}

// ── AdminUpdateSandboxTemplate ───────────────────────────────────────────────

func (s *templateServer) AdminUpdateSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminUpdateSandboxTemplateRequestObject,
) (wsproxygen.AdminUpdateSandboxTemplateResponseObject, error) {
	if s.m.deps.TemplateService == nil {
		return wsproxygen.AdminUpdateSandboxTemplate503JSONResponse{
			Error: "template sync not configured",
		}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminUpdateSandboxTemplate403JSONResponse{
			Error: "admin access required",
		}, nil
	}

	crdJSON := request.Body.CrdJson
	desired, parseErr := parseCRDJSONToK8s(crdJSON)
	if parseErr != nil {
		return wsproxygen.AdminUpdateSandboxTemplate400JSONResponse{
			Error: parseErr.Error(),
		}, nil
	}
	desired.Name = request.Name
	injectGlobalLabel(desired)

	result, appErr := s.m.deps.TemplateService.Update(ctx, desired)
	if appErr != nil {
		log.Printf("syncManager: template update error: %v", appErr)
		switch appErrStatus(appErr) {
		case 400:
			return wsproxygen.AdminUpdateSandboxTemplate400JSONResponse{Error: appErr.Message}, nil
		case 404:
			return wsproxygen.AdminUpdateSandboxTemplate404JSONResponse{Error: appErr.Message}, nil
		case 409:
			return wsproxygen.AdminUpdateSandboxTemplate409JSONResponse{Error: appErr.Message}, nil
		default:
			return wsproxygen.AdminUpdateSandboxTemplate503JSONResponse{Error: appErr.Message}, nil
		}
	}

	s.broadcastDomainTemplate(result)
	genTmpl := service.TemplateToGen(result)
	return wsproxygen.AdminUpdateSandboxTemplate200JSONResponse{Template: genTmpl}, nil
}

// ── AdminDeleteSandboxTemplate ───────────────────────────────────────────────

func (s *templateServer) AdminDeleteSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminDeleteSandboxTemplateRequestObject,
) (wsproxygen.AdminDeleteSandboxTemplateResponseObject, error) {
	if s.m.deps.TemplateService == nil {
		return wsproxygen.AdminDeleteSandboxTemplate503JSONResponse{
			Error: "template sync not configured",
		}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminDeleteSandboxTemplate403JSONResponse{
			Error: "admin access required",
		}, nil
	}

	if appErr := s.m.deps.TemplateService.Delete(ctx, request.Name); appErr != nil {
		log.Printf("syncManager: template delete error: %v", appErr)
		if appErrStatus(appErr) == 404 {
			return wsproxygen.AdminDeleteSandboxTemplate404JSONResponse{Error: appErr.Message}, nil
		}
		return wsproxygen.AdminDeleteSandboxTemplate503JSONResponse{Error: appErr.Message}, nil
	}
	s.m.broadcast(protocol.Frame{Type: protocol.FrameTemplateDeleteSync, Name: request.Name})
	return wsproxygen.AdminDeleteSandboxTemplate204Response{}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// parseCRDJSONToK8s deserializes a JSON string into an agentsv1alpha1.SandboxTemplate
// and validates that metadata.name is present.
func parseCRDJSONToK8s(jsonStr string) (*agentsv1alpha1.SandboxTemplate, error) {
	var tmpl agentsv1alpha1.SandboxTemplate
	if err := json.Unmarshal([]byte(jsonStr), &tmpl); err != nil {
		return nil, fmt.Errorf("invalid CRD JSON: %w", err)
	}
	if tmpl.Name == "" {
		return nil, fmt.Errorf("CRD JSON must include metadata.name")
	}
	return &tmpl, nil
}

func injectGlobalLabel(tmpl *agentsv1alpha1.SandboxTemplate) {
	if tmpl.Labels == nil {
		tmpl.Labels = make(map[string]string)
	}
	tmpl.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal
}

// broadcastDomainTemplate sends a FrameTemplateSync to all connected Worker clusters.
func (s *templateServer) broadcastDomainTemplate(result *domain.SandboxTemplate) {
	if sf, fErr := templateDomainToFrame(result); fErr == nil {
		sf.Type = protocol.FrameTemplateSync
		s.m.broadcast(sf)
	} else {
		log.Printf("syncManager: failed to build template broadcast frame: %v", fErr)
	}
}
