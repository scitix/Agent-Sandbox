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
	"sigs.k8s.io/yaml"
)

// ── ListSandboxTemplates ─────────────────────────────────────────────────────

func (s *Server) ListSandboxTemplates(
	ctx context.Context,
	_ wsproxygen.ListSandboxTemplatesRequestObject,
) (wsproxygen.ListSandboxTemplatesResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.TemplateService == nil {
		return wsproxygen.ListSandboxTemplates503JSONResponse{Error: "template sync not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	items, appErr := deps.TemplateService.List(ctx, auth, isAdmin)
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

func (s *Server) GetSandboxTemplate(
	ctx context.Context,
	request wsproxygen.GetSandboxTemplateRequestObject,
) (wsproxygen.GetSandboxTemplateResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.TemplateService == nil {
		return wsproxygen.GetSandboxTemplate503JSONResponse{Error: "template sync not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	tmpl, appErr := deps.TemplateService.Get(ctx, request.Name, auth, isAdmin)
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

func (s *Server) AdminCreateSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminCreateSandboxTemplateRequestObject,
) (wsproxygen.AdminCreateSandboxTemplateResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.TemplateService == nil {
		return wsproxygen.AdminCreateSandboxTemplate503JSONResponse{Error: "template sync not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminCreateSandboxTemplate403JSONResponse{Error: "admin access required"}, nil
	}

	tmpl, parseErr := parseCRDJSONToK8s(request.Body.CrdJson)
	if parseErr != nil {
		return wsproxygen.AdminCreateSandboxTemplate400JSONResponse{Error: parseErr.Error()}, nil
	}
	injectGlobalLabel(tmpl)

	result, appErr := deps.TemplateService.Create(ctx, tmpl)
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

func (s *Server) AdminUpdateSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminUpdateSandboxTemplateRequestObject,
) (wsproxygen.AdminUpdateSandboxTemplateResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.TemplateService == nil {
		return wsproxygen.AdminUpdateSandboxTemplate503JSONResponse{Error: "template sync not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminUpdateSandboxTemplate403JSONResponse{Error: "admin access required"}, nil
	}

	desired, parseErr := parseCRDJSONToK8s(request.Body.CrdJson)
	if parseErr != nil {
		return wsproxygen.AdminUpdateSandboxTemplate400JSONResponse{Error: parseErr.Error()}, nil
	}
	desired.Name = request.Name
	injectGlobalLabel(desired)

	result, appErr := deps.TemplateService.Update(ctx, desired)
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

func (s *Server) AdminDeleteSandboxTemplate(
	ctx context.Context,
	request wsproxygen.AdminDeleteSandboxTemplateRequestObject,
) (wsproxygen.AdminDeleteSandboxTemplateResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.TemplateService == nil {
		return wsproxygen.AdminDeleteSandboxTemplate503JSONResponse{Error: "template sync not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminDeleteSandboxTemplate403JSONResponse{Error: "admin access required"}, nil
	}

	if appErr := deps.TemplateService.Delete(ctx, request.Name); appErr != nil {
		log.Printf("syncManager: template delete error: %v", appErr)
		if appErrStatus(appErr) == 404 {
			return wsproxygen.AdminDeleteSandboxTemplate404JSONResponse{Error: appErr.Message}, nil
		}
		return wsproxygen.AdminDeleteSandboxTemplate503JSONResponse{Error: appErr.Message}, nil
	}
	s.m.Broadcast(protocol.Frame{Type: protocol.FrameTemplateDeleteSync, Name: request.Name})
	return wsproxygen.AdminDeleteSandboxTemplate204Response{}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

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

func appErrStatus(appErr *domain.AppError) int {
	if appErr == nil {
		return 0
	}
	return int(appErr.Code)
}

func (s *Server) broadcastDomainTemplate(result *domain.SandboxTemplate) {
	if result.CrdYaml == "" {
		log.Printf("syncManager: template %q has empty CrdYaml, skipping broadcast", result.Name)
		return
	}
	var obj any
	if err := yaml.Unmarshal([]byte(result.CrdYaml), &obj); err != nil {
		log.Printf("syncManager: failed to unmarshal CrdYaml for broadcast: %v", err)
		return
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		log.Printf("syncManager: failed to marshal CrdYaml JSON for broadcast: %v", err)
		return
	}
	s.m.Broadcast(protocol.Frame{Type: protocol.FrameTemplateSync, TemplateFull: raw})
}
