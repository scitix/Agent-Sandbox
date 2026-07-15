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

// Package handlers implements the StrictServerInterface generated from the OpenAPI spec.
// Each handler delegates to the service layer and converts domain types to generated types.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envautoscaler"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	"github.com/scitix/agent-sandbox/pkg/utils/httplog"
	k8sname "github.com/scitix/agent-sandbox/pkg/utils/k8sname"
)

// templateLog is the structured logger for admin template handlers.
var templateLog = ctrl.Log.WithName("template-handler")

// adminUser is the built-in admin username used for pure admin auth checks.
const adminUser = "admin"

// Services bundles all service interfaces needed by the handler Server.
type Services struct {
	Sandbox         service.SandboxService
	SandboxEnv      service.SandboxEnvService
	SandboxTemplate service.SandboxTemplateService
	APIKey          service.APIKeyService
	Quota           service.QuotaService
	Organization    service.OrganizationService
	// Sync is the SyncService used to forward admin template write operations to
	// ws-proxy (master cluster). When nil, operations fall back to local mode.
	Sync service.SyncService
	// Forwarder enables cross-cluster forwarding for Native API requests.
	// When nil, cross-cluster operations return 400.
	Forwarder *service.CrossClusterForwarder
	// Cluster serves the /v1/clusters catalog endpoint. Must be non-nil.
	Cluster service.ClusterService
	// QuotaProvider drives the /v1/feature-gates endpoint (and in the future
	// may be used to short-circuit feature-specific handlers when disabled).
	// Nil is treated as Noop.
	QuotaProvider quotaplugin.Provider
	// InstanceTypeProvider backs the /v1/instancetypes listing and the
	// matching `instanceType` boolean on /v1/feature-gates. Nil is treated as
	// Noop (catalog reported disabled, list returns empty).
	InstanceTypeProvider instancetypeplugin.Provider
}

// Server implements gen.StrictServerInterface.
type Server struct {
	sandbox      service.SandboxService
	env          service.SandboxEnvService
	template     service.SandboxTemplateService
	apikey       service.APIKeyService
	quota        service.QuotaService
	organization service.OrganizationService
	// sync is non-nil when this Worker is connected to ws-proxy for global template management.
	sync service.SyncService
	// forwarder handles cross-cluster forwarding for Native API requests.
	forwarder *service.CrossClusterForwarder
	// cluster serves the /v1/clusters catalog endpoint.
	cluster service.ClusterService
	// quotaProvider backs the /v1/feature-gates report.
	// Never nil — NewServer defaults it to Noop when the caller omits it.
	quotaProvider quotaplugin.Provider
	// instanceTypeProvider backs the /v1/instancetypes listing and the
	// `instanceType` boolean on /v1/feature-gates. Never nil — NewServer
	// defaults it to Noop when the caller omits it.
	instanceTypeProvider instancetypeplugin.Provider
}

// NewServer creates a new handler Server.
func NewServer(svcs Services) *Server {
	qp := svcs.QuotaProvider
	if qp == nil {
		qp = quotaplugin.NewNoop()
	}
	itp := svcs.InstanceTypeProvider
	if itp == nil {
		itp = instancetypeplugin.NewNoop()
	}
	return &Server{
		sandbox:              svcs.Sandbox,
		env:                  svcs.SandboxEnv,
		template:             svcs.SandboxTemplate,
		apikey:               svcs.APIKey,
		quota:                svcs.Quota,
		organization:         svcs.Organization,
		sync:                 svcs.Sync,
		forwarder:            svcs.Forwarder,
		cluster:              svcs.Cluster,
		quotaProvider:        qp,
		instanceTypeProvider: itp,
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// authFrom extracts AuthInfo from the context.
// Works with both raw *gin.Context and wrapped contexts.
func authFrom(ctx context.Context) domain.AuthInfo {
	return httpctx.AuthFrom(ctx)
}

func errResp(ctx context.Context, appErr *domain.AppError) gen.ErrorResponse {
	httplog.LogAppError(httpctx.GinFromCtx(ctx), appErr)
	resp := gen.ErrorResponse{Error: appErr.Message, Detail: appErr.Detail}
	if appErr.BizCode != "" {
		s := string(appErr.BizCode)
		resp.ErrorCode = &s
	}
	return resp
}

// jsonBody marshals v to JSON and returns it as an io.Reader for use as a
// forwarded request body. If marshaling fails the body is silently empty —
// the remote cluster will return a validation error which is more informative
// than a local 500.
func jsonBody(v any) io.Reader {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return bytes.NewReader(b)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// templateToSummary converts a gen.SandboxTemplate (full) to gen.SandboxTemplateSummary
// (omits docs / crdYaml, derives hasDocs).
func templateToSummary(t *gen.SandboxTemplate) gen.SandboxTemplateSummary {
	summary := gen.SandboxTemplateSummary{
		Name:        t.Name,
		Version:     t.Version,
		Description: t.Description,
		Cpu:         t.Cpu,
		Memory:      t.Memory,
		SyncSource:  t.SyncSource,
		CreatedAt:   t.CreatedAt,
	}
	hasDocs := t.Docs != nil && strings.TrimSpace(*t.Docs) != ""
	summary.HasDocs = &hasDocs
	return summary
}

// parseCRDJSON deserialises a complete SandboxTemplate CRD JSON string into an
// agentsv1alpha1.SandboxTemplate. metadata.name must be present.
func parseCRDJSON(jsonStr string) (*agentsv1alpha1.SandboxTemplate, error) {
	var tmpl agentsv1alpha1.SandboxTemplate
	if err := json.Unmarshal([]byte(jsonStr), &tmpl); err != nil {
		return nil, fmt.Errorf("invalid CRD JSON: %w", err)
	}
	if tmpl.Name == "" {
		return nil, fmt.Errorf("CRD JSON must include metadata.name")
	}
	return &tmpl, nil
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func (s *Server) GetWhoAmI(ctx context.Context, _ gen.GetWhoAmIRequestObject) (gen.GetWhoAmIResponseObject, error) {
	auth := authFrom(ctx)
	return gen.GetWhoAmI200JSONResponse(gen.WhoAmIResult{
		Role: auth.Role,
		User: ptr.To(auth.User),
		Team: ptr.To(auth.Team),
	}), nil
}

// ---------------------------------------------------------------------------
// Sandboxes
// ---------------------------------------------------------------------------

// isCrossCluster reports whether clusterID refers to a remote cluster that
// requires forwarding. Delegates to the forwarder's nil-safe IsCrossCluster.
func (s *Server) isCrossCluster(clusterID string) bool {
	return s.forwarder.IsCrossCluster(clusterID)
}

// forwardEnvCreateIfRemote handles Env-based cross-cluster placement for a
// Sandbox.Create. For a bare Env name (no explicit "cluster::" prefix), it asks
// the router whether a same-named Env in another cluster has idle capacity
// while the local Env has none; if so it pins the exact foreign member pool,
// rewrites the request to "<cluster>::<pool>", and forwards it — returning
// true. Reusing the explicit cluster::pool form means the receiving cluster
// resolves it as a direct local pool (never re-forwarding: structurally
// single-hop) and prefixes the returned sandbox ID with its own cluster, so
// subsequent operations route back. A request that already carries a cluster
// prefix is left for the direct-forward path.
func (s *Server) forwardEnvCreateIfRemote(ctx context.Context, parsed cluster.ParsedPoolRef, namespace, scalingGroup string, body *gen.CreateSandboxRequest) bool {
	if parsed.ClusterID != "" {
		return false
	}
	r, ok := s.sandbox.(interface {
		ResolveCreateTarget(namespace, poolOrEnvName, scalingGroup string) (string, string, bool)
	})
	if !ok {
		return false
	}
	targetCluster, targetPool, found := r.ResolveCreateTarget(namespace, parsed.PoolName, scalingGroup)
	if !found || !s.isCrossCluster(targetCluster) {
		return false
	}
	// Rewrite the forwarded pool reference to the pinned foreign pool so the
	// receiver treats it as an explicit cluster::pool create.
	fwd := *body
	fwd.PoolName = targetCluster + "::" + targetPool
	s.forwarder.Forward(httpctx.GinFromCtx(ctx), targetCluster, service.URLKindNative, jsonBody(&fwd))
	return true
}

func (s *Server) CreateSandbox(ctx context.Context, req gen.CreateSandboxRequestObject) (gen.CreateSandboxResponseObject, error) {
	auth := authFrom(ctx)
	if req.Body == nil || strings.TrimSpace(req.Body.PoolName) == "" {
		return gen.CreateSandbox400JSONResponse{Error: "poolName is required"}, nil
	}

	parsed := cluster.ParsePoolRef(req.Body.PoolName)
	if parsed.PoolName == "" {
		return gen.CreateSandbox400JSONResponse{Error: "poolName is required"}, nil
	}

	// Cross-cluster: forward the raw request immediately, before any further processing.
	if s.isCrossCluster(parsed.ClusterID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), parsed.ClusterID, service.URLKindNative, jsonBody(req.Body))
		return nil, nil
	}

	input := service.CreateSandboxInput{
		ClusterID: parsed.ClusterID,
		PoolName:  parsed.PoolName,
		Namespace: auth.Namespace,
		Image:     derefString(req.Body.Image),
	}
	if req.Body.ContainerImages != nil {
		input.ContainerImages = *req.Body.ContainerImages
	}
	if req.Body.Labels != nil {
		input.Labels = *req.Body.Labels
	} else {
		input.Labels = make(map[string]string)
	}
	// Inject team/user from auth into labels so they are recorded on the pod.
	if auth.Team != "" {
		input.Labels[agentsv1alpha1.LabelTeam] = auth.Team
	}
	if auth.User != "" {
		input.Labels[agentsv1alpha1.LabelUser] = auth.User
	}
	if req.Body.Annotations != nil {
		input.Annotations = *req.Body.Annotations
	}
	if req.Body.Metadata != nil {
		input.Metadata = *req.Body.Metadata
		// Scaling-group placement via the reserved metadata key (mirrors the
		// E2B handler): pin selection to one autoscaling group, then strip the
		// key so it isn't persisted as sandbox metadata.
		if grp, ok := input.Metadata[service.MetaKeyScalingGroup]; ok && grp != "" {
			input.RequestedScalingGroup = grp
			delete(input.Metadata, service.MetaKeyScalingGroup)
		}
	}
	if req.Body.IdleTimeout != nil && *req.Body.IdleTimeout != "" {
		d, err := time.ParseDuration(*req.Body.IdleTimeout)
		if err != nil || d <= 0 {
			return gen.CreateSandbox400JSONResponse{Error: "idleTimeout must be a positive duration"}, nil
		}
		input.IdleTimeout = d
	}
	if req.Body.StartupTimeout != nil && *req.Body.StartupTimeout != "" {
		d, err := time.ParseDuration(*req.Body.StartupTimeout)
		if err != nil || d <= 0 {
			return gen.CreateSandbox400JSONResponse{Error: "startupTimeout must be a positive duration"}, nil
		}
		input.StartupTimeout = d
	}

	// Env-based cross-cluster placement: forward to a same-named Env in
	// another cluster when the local Env has no idle capacity.
	if s.forwardEnvCreateIfRemote(ctx, parsed, input.Namespace, input.RequestedScalingGroup, req.Body) {
		return nil, nil
	}

	result, appErr := s.sandbox.Create(ctx, input)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.CreateSandbox404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.CreateSandbox409JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeBadRequest:
			return gen.CreateSandbox400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeTooManyRequests:
			return gen.CreateSandbox429JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.CreateSandbox500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.CreateSandbox201JSONResponse{Sandbox: *result}, nil
}

func (s *Server) ListSandboxes(ctx context.Context, req gen.ListSandboxesRequestObject) (gen.ListSandboxesResponseObject, error) {
	auth := authFrom(ctx)
	limit := 20
	if req.Params.Limit != nil {
		limit = min(*req.Params.Limit, 100)
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}
	filter := service.SandboxListFilter{
		Namespace: auth.Namespace,
		Team:      auth.Team,
		User:      auth.User,
		Limit:     limit,
		Offset:    offset,
	}
	if req.Params.PoolName != nil {
		filter.PoolName = *req.Params.PoolName
	}
	if req.Params.Status != nil {
		filter.Status = *req.Params.Status
	}

	result, appErr := s.sandbox.List(ctx, filter)
	if appErr != nil {
		return gen.ListSandboxes500JSONResponse(errResp(ctx, appErr)), nil
	}

	return gen.ListSandboxes200JSONResponse{
		Items:  result.Items,
		Total:  result.Total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *Server) GetSandbox(ctx context.Context, req gen.GetSandboxRequestObject) (gen.GetSandboxResponseObject, error) {
	auth := authFrom(ctx)
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}
	result, appErr := s.sandbox.Get(ctx, auth.Namespace, req.SandboxId)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandbox404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandbox500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.GetSandbox200JSONResponse{Sandbox: *result}, nil
}

func (s *Server) GetSandboxLogs(ctx context.Context, req gen.GetSandboxLogsRequestObject) (gen.GetSandboxLogsResponseObject, error) {
	auth := authFrom(ctx)
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}

	result, appErr := s.sandbox.GetLogs(ctx, auth.Namespace, req.SandboxId, req.Params)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandboxLogs404JSONResponse(errResp(ctx, appErr)), nil
		}
		if appErr.Code == domain.ErrCodeBadRequest {
			return gen.GetSandboxLogs400JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandboxLogs500JSONResponse(errResp(ctx, appErr)), nil
	}

	return gen.GetSandboxLogs200JSONResponse(*result), nil
}

func (s *Server) DeleteSandbox(ctx context.Context, req gen.DeleteSandboxRequestObject) (gen.DeleteSandboxResponseObject, error) {
	auth := authFrom(ctx)
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}
	result, appErr := s.sandbox.Delete(ctx, auth.Namespace, req.SandboxId)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.DeleteSandbox404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.DeleteSandbox500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.DeleteSandbox202JSONResponse{
		SandboxId: result.SandboxId,
		Namespace: result.Namespace,
		PoolName:  result.PoolName,
		PodName:   result.PodName,
		Status:    result.Status,
	}, nil
}

func (s *Server) SetSandboxTimeout(ctx context.Context, req gen.SetSandboxTimeoutRequestObject) (gen.SetSandboxTimeoutResponseObject, error) {
	auth := authFrom(ctx)
	if req.Body == nil {
		return gen.SetSandboxTimeout401JSONResponse{Error: "request body is required"}, nil
	}
	var d time.Duration
	if req.Body.Timeout != "0" {
		var parseErr error
		d, parseErr = time.ParseDuration(req.Body.Timeout)
		if parseErr != nil {
			return gen.SetSandboxTimeout401JSONResponse{Error: "invalid timeout: " + parseErr.Error()}, nil
		}
	}
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, jsonBody(req.Body))
		return nil, nil
	}
	if appErr := s.sandbox.SetTimeout(ctx, auth.Namespace, req.SandboxId, d); appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.SetSandboxTimeout404JSONResponse{Error: appErr.Message}, nil
		}
		return gen.SetSandboxTimeout500JSONResponse{Error: appErr.Message}, nil
	}
	return gen.SetSandboxTimeout204Response{}, nil
}

func (s *Server) CreateSandboxExecToken(ctx context.Context, req gen.CreateSandboxExecTokenRequestObject) (gen.CreateSandboxExecTokenResponseObject, error) {
	auth := authFrom(ctx)
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}
	token, appErr := s.sandbox.CreateExecToken(ctx, auth.Namespace, req.SandboxId)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.CreateSandboxExecToken404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeBadRequest:
			return gen.CreateSandboxExecToken400JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.CreateSandboxExecToken500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.CreateSandboxExecToken200JSONResponse{Token: token}, nil
}

// ---------------------------------------------------------------------------
// SandboxPools
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// SandboxEnv
// ---------------------------------------------------------------------------

func (s *Server) ListSandboxEnvs(ctx context.Context, _ gen.ListSandboxEnvsRequestObject) (gen.ListSandboxEnvsResponseObject, error) {
	auth := authFrom(ctx)
	items, appErr := s.env.List(ctx, auth.Namespace, auth.Team, auth.User)
	if appErr != nil {
		return gen.ListSandboxEnvs500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListSandboxEnvs200JSONResponse{Items: items}, nil
}

func (s *Server) GetSandboxEnv(ctx context.Context, req gen.GetSandboxEnvRequestObject) (gen.GetSandboxEnvResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.env.Get(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandboxEnv404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandboxEnv500JSONResponse(errResp(ctx, appErr)), nil
	}

	// Render env docs template on the server so the client gets a
	// ready-to-copy snippet. When the raw docs reference ${AGBX_API_KEY} and
	// the user has no key with a recoverable plaintext token, return
	// API_KEY_REQUIRED (422) so the frontend can guide them to the API Keys
	// page.
	raw := ""
	if result.EnvDocs != nil {
		raw = *result.EnvDocs
	}
	rendered, renderErr := s.renderEnvDocs(ctx, raw, result.Name, s.forwarder.LocalClusterID(), auth)
	if renderErr != nil {
		switch renderErr.Code {
		case domain.ErrCodeUnprocessableEntity:
			return gen.GetSandboxEnv422JSONResponse(errResp(ctx, renderErr)), nil
		default:
			return gen.GetSandboxEnv500JSONResponse(errResp(ctx, renderErr)), nil
		}
	}
	if rendered != "" {
		result.EnvDocs = ptr.To(rendered)
	} else {
		result.EnvDocs = nil
	}

	return gen.GetSandboxEnv200JSONResponse{Env: *result}, nil
}

func (s *Server) CreateSandboxEnv(ctx context.Context, req gen.CreateSandboxEnvRequestObject) (gen.CreateSandboxEnvResponseObject, error) {
	if req.Body == nil {
		return gen.CreateSandboxEnv400JSONResponse{Error: "request body is required"}, nil
	}
	if req.Body.Name == "" {
		return gen.CreateSandboxEnv400JSONResponse{Error: "name is required"}, nil
	}
	if req.Body.TemplateRef.Name == "" {
		return gen.CreateSandboxEnv400JSONResponse{Error: "templateRef.name is required"}, nil
	}
	auth := authFrom(ctx)

	input := service.CreateSandboxEnvInput{
		Name:      req.Body.Name,
		Namespace: auth.Namespace,
		Team:      auth.Team,
		User:      auth.User,
		TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{
			Name: req.Body.TemplateRef.Name,
		},
	}
	if req.Body.TemplateRef.Version != nil {
		input.TemplateRef.Version = *req.Body.TemplateRef.Version
	}
	if req.Body.Mode != nil {
		input.Mode = agentsv1alpha1.SandboxEnvMode(*req.Body.Mode)
	}
	if req.Body.Overrides != nil {
		ov, err := envOverridesFromGen(req.Body.Overrides)
		if err != nil {
			return gen.CreateSandboxEnv400JSONResponse{Error: err.Error()}, nil
		}
		input.Overrides = ov
		input.ImagePullSecret = req.Body.Overrides.ImagePullSecret
	}
	if req.Body.Labels != nil {
		input.Labels = *req.Body.Labels
	}
	if req.Body.Annotations != nil {
		input.Annotations = *req.Body.Annotations
	}

	result, appErr := s.env.Create(ctx, input)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.CreateSandboxEnv400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.CreateSandboxEnv409JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.CreateSandboxEnv500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.CreateSandboxEnv201JSONResponse{Env: *result}, nil
}

func (s *Server) UpdateSandboxEnv(ctx context.Context, req gen.UpdateSandboxEnvRequestObject) (gen.UpdateSandboxEnvResponseObject, error) {
	if req.Body == nil {
		return gen.UpdateSandboxEnv400JSONResponse{Error: "request body is required"}, nil
	}
	auth := authFrom(ctx)
	input := service.UpdateSandboxEnvInput{
		Name:      req.Name,
		Namespace: auth.Namespace,
	}
	if req.Body.Overrides != nil {
		ov, err := envOverridesFromGen(req.Body.Overrides)
		if err != nil {
			return gen.UpdateSandboxEnv400JSONResponse{Error: err.Error()}, nil
		}
		input.Overrides = ov
		input.ImagePullSecret = req.Body.Overrides.ImagePullSecret
	}
	if input.Overrides == nil && input.ImagePullSecret == nil {
		return gen.UpdateSandboxEnv400JSONResponse{Error: "at least one editable field must be provided"}, nil
	}

	result, appErr := s.env.Update(ctx, input)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.UpdateSandboxEnv400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.UpdateSandboxEnv404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.UpdateSandboxEnv500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.UpdateSandboxEnv200JSONResponse{Env: *result}, nil
}

func (s *Server) DeleteSandboxEnv(ctx context.Context, req gen.DeleteSandboxEnvRequestObject) (gen.DeleteSandboxEnvResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.env.Delete(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.DeleteSandboxEnv404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.DeleteSandboxEnv500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.DeleteSandboxEnv202JSONResponse(*result), nil
}

// envOverridesFromGen projects the wire Env-level Overrides into the CRD
// shape. Duration strings are parsed eagerly so a malformed value surfaces
// at the boundary.
func envOverridesFromGen(o *gen.EnvOverrides) (*agentsv1alpha1.EnvOverridesSpec, error) {
	if o == nil {
		return nil, nil
	}
	out := &agentsv1alpha1.EnvOverridesSpec{}
	if o.Image != nil {
		out.Image = *o.Image
	}
	if o.PodCreationImagePolicy != nil {
		out.PodCreationImagePolicy = agentsv1alpha1.PodCreationImagePolicy(*o.PodCreationImagePolicy)
	}
	if o.DefaultStartupTimeout != nil {
		d, err := time.ParseDuration(*o.DefaultStartupTimeout)
		if err != nil {
			return nil, fmt.Errorf("overrides.defaultStartupTimeout: %v", err)
		}
		out.DefaultStartupTimeout = &metav1.Duration{Duration: d}
	}
	if o.DefaultIdleTimeout != nil {
		d, err := time.ParseDuration(*o.DefaultIdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("overrides.defaultIdleTimeout: %v", err)
		}
		out.DefaultIdleTimeout = &metav1.Duration{Duration: d}
	}
	out.NetworkPolicy = networkPolicyFromGen(o.NetworkPolicy)
	out.UpdateStrategy = updateStrategyFromGen(o.UpdateStrategy)
	return out, nil
}

// updateStrategyFromGen maps the wire rollout policy onto the CRD spec.
// MaxUnavailable is parsed from its int-or-percent string form ("3" / "20%").
func updateStrategyFromGen(s *gen.EnvUpdateStrategy) *agentsv1alpha1.EnvUpdateStrategy {
	if s == nil {
		return nil
	}
	out := &agentsv1alpha1.EnvUpdateStrategy{AutoUpdate: s.AutoUpdate}
	if s.MaxUnavailable != nil && *s.MaxUnavailable != "" {
		v := intstr.Parse(*s.MaxUnavailable)
		out.MaxUnavailable = &v
	}
	return out
}

// networkPolicyFromGen maps the wire egress policy onto the CRD spec.
func networkPolicyFromGen(g *gen.SandboxNetworkPolicy) *agentsv1alpha1.SandboxNetworkPolicy {
	if g == nil {
		return nil
	}
	np := &agentsv1alpha1.SandboxNetworkPolicy{}
	if g.DisableEgress != nil {
		np.DisableEgress = *g.DisableEgress
	}
	if g.AllowPrivateNetworks != nil {
		np.AllowPrivateNetworks = *g.AllowPrivateNetworks
	}
	if g.Egress != nil {
		e := &agentsv1alpha1.EgressRules{}
		if g.Egress.AllowedDomains != nil {
			e.AllowedDomains = *g.Egress.AllowedDomains
		}
		if g.Egress.AllowedCIDRs != nil {
			e.AllowedCIDRs = *g.Egress.AllowedCIDRs
		}
		if g.Egress.DeniedCIDRs != nil {
			e.DeniedCIDRs = *g.Egress.DeniedCIDRs
		}
		np.Egress = e
	}
	return np
}

// inlineResourcesFromGen projects the wire ResourceRequirements (Quantity
// strings keyed by resource name) into the corev1 shape consumed by the
// renderer. Invalid quantities are dropped silently — the CRD validation
// catches them when the patch reaches the apiserver.
func inlineResourcesFromGen(in *gen.ResourceRequirements) *corev1.ResourceRequirements {
	if in == nil {
		return nil
	}
	out := &corev1.ResourceRequirements{}
	if in.Requests != nil {
		out.Requests = quantityMapFromGen(*in.Requests)
	}
	if in.Limits != nil {
		out.Limits = quantityMapFromGen(*in.Limits)
	}
	if len(out.Requests) == 0 && len(out.Limits) == 0 {
		return nil
	}
	return out
}

func quantityMapFromGen(m map[string]string) corev1.ResourceList {
	if len(m) == 0 {
		return nil
	}
	out := corev1.ResourceList{}
	for k, v := range m {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			continue
		}
		out[corev1.ResourceName(k)] = q
	}
	return out
}

// ---------------------------------------------------------------------------
// SandboxEnv events
// ---------------------------------------------------------------------------

func (s *Server) ListEnvEvents(ctx context.Context, req gen.ListEnvEventsRequestObject) (gen.ListEnvEventsResponseObject, error) {
	auth := authFrom(ctx)
	limit := 0
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	items, appErr := s.env.ListEvents(ctx, auth.Namespace, req.Name, limit)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.ListEnvEvents404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.ListEnvEvents500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.ListEnvEvents200JSONResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// ---------------------------------------------------------------------------
// Env-scoped SandboxPools
// ---------------------------------------------------------------------------

// memberFromCreateEnvPoolRequest converts the wire create-request into the
// CRD shape consumed by AddMemberPool. The server derives Name and
// Config.ScalingGroup downstream — those fields are intentionally not
// populated here; Metadata + Spec are filled by the service layer after
// PreCreatePool admission runs.
func memberFromCreateEnvPoolRequest(body *gen.CreateEnvSandboxPoolRequest) agentsv1alpha1.EnvClusterMember {
	var cm agentsv1alpha1.EnvClusterMember
	if body.InstanceType != nil {
		cm.Config.InstanceType = *body.InstanceType
	}
	if body.Multiplier != nil {
		cm.Config.Multiplier = *body.Multiplier
	}
	if body.MinReplicas != nil {
		v := *body.MinReplicas
		cm.Config.MinReplicas = &v
	}
	if body.MaxReplicas != nil {
		v := *body.MaxReplicas
		cm.Config.MaxReplicas = &v
	}
	if body.Replicas != nil {
		// Initial replica count flows directly into Member.Spec — the
		// Reconciler is the sole writer of the live Pool's Spec.Replicas
		// and reads it from here on every reconcile.
		cm.Spec.Replicas = *body.Replicas
	}
	if body.InlineResources != nil {
		cm.Config.InlineResources = inlineResourcesFromGen(body.InlineResources)
	}
	if body.Labels != nil {
		cm.Config.Labels = *body.Labels
	}
	if body.Annotations != nil {
		cm.Config.Annotations = *body.Annotations
	}
	cm.Config.UpdateStrategy = updateStrategyFromGen(body.UpdateStrategy)
	return cm
}

func (s *Server) CreateEnvSandboxPool(ctx context.Context, req gen.CreateEnvSandboxPoolRequestObject) (gen.CreateEnvSandboxPoolResponseObject, error) {
	if req.Body == nil {
		return gen.CreateEnvSandboxPool400JSONResponse{Error: "request body is required"}, nil
	}
	auth := authFrom(ctx)
	member := memberFromCreateEnvPoolRequest(req.Body)
	result, appErr := s.env.AddMember(ctx, auth.Namespace, req.Name, s.forwarder.LocalClusterID(), member)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.CreateEnvSandboxPool400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.CreateEnvSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.CreateEnvSandboxPool409JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeServiceUnavailable:
			return gen.CreateEnvSandboxPool503JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.CreateEnvSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.CreateEnvSandboxPool202JSONResponse{Template: *result}, nil
}

func (s *Server) ListEnvSandboxPools(ctx context.Context, req gen.ListEnvSandboxPoolsRequestObject) (gen.ListEnvSandboxPoolsResponseObject, error) {
	auth := authFrom(ctx)
	items, appErr := s.env.ListMembers(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.ListEnvSandboxPools404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.ListEnvSandboxPools500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.ListEnvSandboxPools200JSONResponse{
		Items:  items,
		Total:  len(items),
		Limit:  0,
		Offset: 0,
	}, nil
}

func (s *Server) GetEnvSandboxPool(ctx context.Context, req gen.GetEnvSandboxPoolRequestObject) (gen.GetEnvSandboxPoolResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.env.GetMember(ctx, auth.Namespace, req.Name, req.PoolName)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.GetEnvSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.GetEnvSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.GetEnvSandboxPool200JSONResponse{Template: *result}, nil
}

func (s *Server) UpdateEnvSandboxPool(ctx context.Context, req gen.UpdateEnvSandboxPoolRequestObject) (gen.UpdateEnvSandboxPoolResponseObject, error) {
	if req.Body == nil {
		return gen.UpdateEnvSandboxPool400JSONResponse{Error: "request body is required"}, nil
	}
	auth := authFrom(ctx)
	patch := envmember.MemberPoolPatch{}
	if req.Body.Replicas != nil {
		v := *req.Body.Replicas
		patch.Replicas = &v
	}
	if req.Body.MinReplicas != nil {
		v := *req.Body.MinReplicas
		patch.MinReplicas = &v
	}
	if req.Body.MaxReplicas != nil {
		v := *req.Body.MaxReplicas
		patch.MaxReplicas = &v
	}
	if req.Body.UpdateStrategy != nil {
		patch.UpdateStrategy = updateStrategyFromGen(req.Body.UpdateStrategy)
	}
	result, appErr := s.env.UpdateMember(ctx, auth.Namespace, req.Name, req.PoolName, s.forwarder.LocalClusterID(), patch)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.UpdateEnvSandboxPool400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.UpdateEnvSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeServiceUnavailable:
			return gen.UpdateEnvSandboxPool503JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.UpdateEnvSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.UpdateEnvSandboxPool200JSONResponse{Template: *result}, nil
}

func (s *Server) DeleteEnvSandboxPool(ctx context.Context, req gen.DeleteEnvSandboxPoolRequestObject) (gen.DeleteEnvSandboxPoolResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.env.DeleteMember(ctx, auth.Namespace, req.Name, req.PoolName, s.forwarder.LocalClusterID())
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.DeleteEnvSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeServiceUnavailable:
			return gen.DeleteEnvSandboxPool503JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.DeleteEnvSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.DeleteEnvSandboxPool202JSONResponse{
		Name:      result.Name,
		Namespace: result.Namespace,
		Status:    result.Status,
	}, nil
}

// ---------------------------------------------------------------------------
// Env-scoped autoscaler config
// ---------------------------------------------------------------------------

func (s *Server) GetEnvAutoscaling(ctx context.Context, req gen.GetEnvAutoscalingRequestObject) (gen.GetEnvAutoscalingResponseObject, error) {
	auth := authFrom(ctx)
	spec, appErr := s.env.GetAutoscaling(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.GetEnvAutoscaling404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.GetEnvAutoscaling500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.GetEnvAutoscaling200JSONResponse{Spec: *spec}, nil
}

func (s *Server) ListEnvAutoscalingGroups(ctx context.Context, req gen.ListEnvAutoscalingGroupsRequestObject) (gen.ListEnvAutoscalingGroupsResponseObject, error) {
	auth := authFrom(ctx)
	items, appErr := s.env.ListAutoscalingGroups(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.ListEnvAutoscalingGroups404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.ListEnvAutoscalingGroups500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.ListEnvAutoscalingGroups200JSONResponse{Items: items, Total: len(items)}, nil
}

func (s *Server) GetEnvAutoscalingGroup(ctx context.Context, req gen.GetEnvAutoscalingGroupRequestObject) (gen.GetEnvAutoscalingGroupResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.env.GetAutoscalingGroup(ctx, auth.Namespace, req.Name, req.GroupName)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.GetEnvAutoscalingGroup404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.GetEnvAutoscalingGroup500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.GetEnvAutoscalingGroup200JSONResponse{Group: *result}, nil
}

func (s *Server) UpdateEnvAutoscalingGroup(ctx context.Context, req gen.UpdateEnvAutoscalingGroupRequestObject) (gen.UpdateEnvAutoscalingGroupResponseObject, error) {
	if req.Body == nil {
		return gen.UpdateEnvAutoscalingGroup400JSONResponse{Error: "request body is required"}, nil
	}
	auth := authFrom(ctx)
	patch := envautoscaler.GroupPatch{
		Enabled:         req.Body.Enabled,
		MinReplicas:     req.Body.MinReplicas,
		MaxReplicas:     req.Body.MaxReplicas,
		ScaleUpPolicy:   req.Body.ScaleUpPolicy,
		ScaleDownPolicy: req.Body.ScaleDownPolicy,
	}
	result, appErr := s.env.UpdateAutoscalingGroup(ctx, auth.Namespace, req.Name, req.GroupName, patch)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.UpdateEnvAutoscalingGroup400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.UpdateEnvAutoscalingGroup404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.UpdateEnvAutoscalingGroup500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.UpdateEnvAutoscalingGroup200JSONResponse{Group: *result}, nil
}

func (s *Server) DeleteEnvAutoscalingGroup(ctx context.Context, req gen.DeleteEnvAutoscalingGroupRequestObject) (gen.DeleteEnvAutoscalingGroupResponseObject, error) {
	auth := authFrom(ctx)
	if appErr := s.env.DeleteAutoscalingGroup(ctx, auth.Namespace, req.Name, req.GroupName); appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.DeleteEnvAutoscalingGroup404JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.DeleteEnvAutoscalingGroup500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.DeleteEnvAutoscalingGroup200JSONResponse{
		Name:   req.GroupName,
		Status: "Deleted",
	}, nil
}

// ---------------------------------------------------------------------------
// SandboxTemplates
// ---------------------------------------------------------------------------

func (s *Server) ListSandboxTemplates(ctx context.Context, _ gen.ListSandboxTemplatesRequestObject) (gen.ListSandboxTemplatesResponseObject, error) {
	auth := authFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	items, appErr := s.template.List(ctx, auth, isAdmin)
	if appErr != nil {
		return gen.ListSandboxTemplates500JSONResponse(errResp(ctx, appErr)), nil
	}
	payloads := make([]gen.SandboxTemplateSummary, 0, len(items))
	for i := range items {
		payloads = append(payloads, templateToSummary(&items[i]))
	}
	return gen.ListSandboxTemplates200JSONResponse{
		Items:  payloads,
		Total:  len(payloads),
		Limit:  0,
		Offset: 0,
	}, nil
}

func (s *Server) GetSandboxTemplate(ctx context.Context, req gen.GetSandboxTemplateRequestObject) (gen.GetSandboxTemplateResponseObject, error) {
	auth := authFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	result, appErr := s.template.Get(ctx, req.Name, auth, isAdmin)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandboxTemplate404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandboxTemplate500JSONResponse(errResp(ctx, appErr)), nil
	}
	if result.Docs != nil {
		rendered := renderTemplateDocs(*result.Docs, s.forwarder.LocalClusterID())
		result.Docs = ptr.To(rendered)
	} else {
		result.Docs = ptr.To(renderTemplateDocs("", s.forwarder.LocalClusterID()))
	}
	return gen.GetSandboxTemplate200JSONResponse{Template: *result}, nil
}

func (s *Server) AdminCreateSandboxTemplate(ctx context.Context, req gen.AdminCreateSandboxTemplateRequestObject) (gen.AdminCreateSandboxTemplateResponseObject, error) {
	if req.Body == nil {
		return gen.AdminCreateSandboxTemplate400JSONResponse{Error: "request body required"}, nil
	}

	tmplObj, parseErr := parseCRDJSON(req.Body.CrdJson)
	if parseErr != nil {
		return gen.AdminCreateSandboxTemplate400JSONResponse{Error: parseErr.Error()}, nil
	}
	if err := k8sname.Validate(tmplObj.Name); err != nil {
		return gen.AdminCreateSandboxTemplate400JSONResponse{Error: err.Error()}, nil
	}

	// When connected to ws-proxy, forward the write to master so it propagates
	// to all clusters. Fall back to local create when sync is not configured.
	if s.sync != nil {
		raw, marshalErr := json.Marshal(tmplObj)
		if marshalErr != nil {
			httplog.LogServerError(httpctx.GinFromCtx(ctx), marshalErr, "failed to serialize template for sync create", "name", tmplObj.Name)
			return gen.AdminCreateSandboxTemplate500JSONResponse{Error: "failed to serialize template"}, nil
		}
		if syncErr := s.sync.RequestTemplateCreate(ctx, raw); syncErr != nil {
			return templateSyncErrToCreateResp(syncErr), nil
		}
		// The template will be synced back to this cluster via template_sync broadcast.
		// Read the locally synced copy to return in the response (best-effort).
		result, appErr := s.template.Get(ctx, tmplObj.Name, domain.AuthInfo{}, true)
		if appErr != nil {
			return gen.AdminCreateSandboxTemplate201JSONResponse{
				Template: gen.SandboxTemplate{Name: tmplObj.Name},
			}, nil
		}
		return gen.AdminCreateSandboxTemplate201JSONResponse{Template: *result}, nil
	}

	result, appErr := s.template.Create(ctx, tmplObj)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.AdminCreateSandboxTemplate400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.AdminCreateSandboxTemplate409JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.AdminCreateSandboxTemplate500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.AdminCreateSandboxTemplate201JSONResponse{Template: *result}, nil
}

func (s *Server) AdminUpdateSandboxTemplate(ctx context.Context, req gen.AdminUpdateSandboxTemplateRequestObject) (gen.AdminUpdateSandboxTemplateResponseObject, error) {
	if req.Body == nil {
		return gen.AdminUpdateSandboxTemplate400JSONResponse{Error: "request body required"}, nil
	}

	tmplObj, parseErr := parseCRDJSON(req.Body.CrdJson)
	if parseErr != nil {
		return gen.AdminUpdateSandboxTemplate400JSONResponse{Error: parseErr.Error()}, nil
	}
	// URL path name takes precedence over whatever name is in the JSON body.
	tmplObj.Name = req.Name

	// Forward to ws-proxy when sync is configured.
	if s.sync != nil {
		raw, marshalErr := json.Marshal(tmplObj)
		if marshalErr != nil {
			httplog.LogServerError(httpctx.GinFromCtx(ctx), marshalErr, "failed to serialize template for sync update", "name", req.Name)
			return gen.AdminUpdateSandboxTemplate500JSONResponse{Error: "failed to serialize template"}, nil
		}
		if syncErr := s.sync.RequestTemplateUpdate(ctx, raw); syncErr != nil {
			return templateSyncErrToUpdateResp(syncErr), nil
		}
		result, appErr := s.template.Get(ctx, req.Name, domain.AuthInfo{}, true)
		if appErr != nil {
			return gen.AdminUpdateSandboxTemplate200JSONResponse{
				Template: gen.SandboxTemplate{Name: req.Name},
			}, nil
		}
		return gen.AdminUpdateSandboxTemplate200JSONResponse{Template: *result}, nil
	}

	result, appErr := s.template.Update(ctx, tmplObj)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.AdminUpdateSandboxTemplate400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.AdminUpdateSandboxTemplate404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.AdminUpdateSandboxTemplate409JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.AdminUpdateSandboxTemplate500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.AdminUpdateSandboxTemplate200JSONResponse{Template: *result}, nil
}

func (s *Server) AdminDeleteSandboxTemplate(ctx context.Context, req gen.AdminDeleteSandboxTemplateRequestObject) (gen.AdminDeleteSandboxTemplateResponseObject, error) {
	// Forward to ws-proxy only when sync is configured AND the template is globally
	// managed. Single-cluster (legacy) templates are deleted locally even if sync is
	// enabled, since the global manager has no record of them.
	if s.sync != nil {
		tmpl, getErr := s.template.Get(ctx, req.Name, domain.AuthInfo{}, true)
		if getErr != nil {
			if getErr.Code == domain.ErrCodeNotFound {
				return gen.AdminDeleteSandboxTemplate404JSONResponse(errResp(ctx, getErr)), nil
			}
			return gen.AdminDeleteSandboxTemplate500JSONResponse(errResp(ctx, getErr)), nil
		}
		if derefString(tmpl.SyncSource) == agentsv1alpha1.LabelSyncSourceGlobal {
			if syncErr := s.sync.RequestTemplateDelete(ctx, req.Name); syncErr != nil {
				return templateSyncErrToDeleteResp(req.Name, syncErr), nil
			}
			return gen.AdminDeleteSandboxTemplate202JSONResponse{Name: req.Name, Status: "Terminating"}, nil
		}
		// Fall through: single-cluster template, delete locally.
	}

	appErr := s.template.Delete(ctx, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.AdminDeleteSandboxTemplate404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.AdminDeleteSandboxTemplate500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.AdminDeleteSandboxTemplate202JSONResponse{Name: req.Name, Status: "Terminating"}, nil
}

func (s *Server) AdminListSandboxTemplates(ctx context.Context, req gen.AdminListSandboxTemplatesRequestObject) (gen.AdminListSandboxTemplatesResponseObject, error) {
	auth := authFrom(ctx)
	// Admin list: apply optional team/user filter from query params while keeping full visibility.
	if req.Params.Team != nil && *req.Params.Team != "" {
		auth.Team = *req.Params.Team
	}
	if req.Params.User != nil && *req.Params.User != "" {
		auth.User = *req.Params.User
	}
	items, appErr := s.template.List(ctx, auth, true)
	if appErr != nil {
		return gen.AdminListSandboxTemplates500JSONResponse(errResp(ctx, appErr)), nil
	}
	payloads := make([]gen.SandboxTemplateSummary, 0, len(items))
	for i := range items {
		payloads = append(payloads, templateToSummary(&items[i]))
	}
	return gen.AdminListSandboxTemplates200JSONResponse{
		Items:  payloads,
		Total:  len(payloads),
		Limit:  0,
		Offset: 0,
	}, nil
}

// templateSyncErrToCreateResp converts a sync forwarding error to the appropriate Create response.
func templateSyncErrToCreateResp(err error) gen.AdminCreateSandboxTemplateResponseObject {
	var httpErr *service.SyncHTTPError
	if errors.As(err, &httpErr) {
		templateLog.Info("sync template create returned HTTP error", "status", httpErr.Status, "message", httpErr.Message)
		switch httpErr.Status {
		case 400:
			return gen.AdminCreateSandboxTemplate400JSONResponse{Error: httpErr.Message}
		case 409:
			return gen.AdminCreateSandboxTemplate409JSONResponse{Error: httpErr.Message}
		case 503:
			return gen.AdminCreateSandboxTemplate500JSONResponse{Error: httpErr.Message}
		}
	}
	if errors.Is(err, service.ErrSyncNotConnected) {
		templateLog.Error(err, "sync template create failed: ws-proxy not connected")
		return gen.AdminCreateSandboxTemplate500JSONResponse{Error: "global template manager unavailable"}
	}
	templateLog.Error(err, "sync template create failed with unexpected error")
	return gen.AdminCreateSandboxTemplate500JSONResponse{Error: err.Error()}
}

// templateSyncErrToUpdateResp converts a sync forwarding error to the appropriate Update response.
func templateSyncErrToUpdateResp(err error) gen.AdminUpdateSandboxTemplateResponseObject {
	var httpErr *service.SyncHTTPError
	if errors.As(err, &httpErr) {
		templateLog.Info("sync template update returned HTTP error", "status", httpErr.Status, "message", httpErr.Message)
		switch httpErr.Status {
		case 400:
			return gen.AdminUpdateSandboxTemplate400JSONResponse{Error: httpErr.Message}
		case 404:
			return gen.AdminUpdateSandboxTemplate404JSONResponse{Error: httpErr.Message}
		case 503:
			return gen.AdminUpdateSandboxTemplate500JSONResponse{Error: httpErr.Message}
		}
	}
	if errors.Is(err, service.ErrSyncNotConnected) {
		templateLog.Error(err, "sync template update failed: ws-proxy not connected")
		return gen.AdminUpdateSandboxTemplate500JSONResponse{Error: "global template manager unavailable"}
	}
	templateLog.Error(err, "sync template update failed with unexpected error")
	return gen.AdminUpdateSandboxTemplate500JSONResponse{Error: err.Error()}
}

// templateSyncErrToDeleteResp converts a sync forwarding error to the appropriate Delete response.
func templateSyncErrToDeleteResp(name string, err error) gen.AdminDeleteSandboxTemplateResponseObject {
	var httpErr *service.SyncHTTPError
	if errors.As(err, &httpErr) {
		templateLog.Info("sync template delete returned HTTP error", "template", name, "status", httpErr.Status, "message", httpErr.Message)
		switch httpErr.Status {
		case 404:
			return gen.AdminDeleteSandboxTemplate404JSONResponse{Error: httpErr.Message}
		case 503:
			return gen.AdminDeleteSandboxTemplate500JSONResponse{Error: httpErr.Message}
		}
	}
	if errors.Is(err, service.ErrSyncNotConnected) {
		templateLog.Error(err, "sync template delete failed: ws-proxy not connected", "template", name)
		return gen.AdminDeleteSandboxTemplate500JSONResponse{Error: "global template manager unavailable"}
	}
	templateLog.Error(err, "sync template delete failed with unexpected error", "template", name)
	return gen.AdminDeleteSandboxTemplate500JSONResponse{Error: err.Error()}
}

// ---------------------------------------------------------------------------
// Quotas
// ---------------------------------------------------------------------------

func (s *Server) ListQuotas(ctx context.Context, _ gen.ListQuotasRequestObject) (gen.ListQuotasResponseObject, error) {
	auth := authFrom(ctx)
	// Admin callers that have not provided impersonation headers will have
	// User="admin" / Team="admin", which is not a real tenant identity.
	// They must use X-Impersonate-Team and X-Impersonate-User headers to
	// query quotas on behalf of a specific user.
	if auth.User == "" || auth.Team == "" || (auth.User == adminUser && auth.Team == adminUser) {
		return gen.ListQuotas403JSONResponse{Error: "quota lookup requires user and team context; admins must provide X-Impersonate-Team and X-Impersonate-User headers"}, nil
	}
	quotas, appErr := s.quota.ListForUser(ctx, auth.User, auth.Team)
	if appErr != nil {
		return gen.ListQuotas500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListQuotas200JSONResponse{
		Items:  quotas,
		Total:  len(quotas),
		Limit:  0,
		Offset: 0,
	}, nil
}

// ---------------------------------------------------------------------------
// API Keys — Self-service (tenant: namespace/user/team locked to auth context)
// ---------------------------------------------------------------------------

func (s *Server) SelfCreateAPIKey(ctx context.Context, req gen.SelfCreateAPIKeyRequestObject) (gen.SelfCreateAPIKeyResponseObject, error) {
	if req.Body == nil {
		req.Body = &gen.SelfCreateAPIKeyRequest{}
	}
	auth := authFrom(ctx)

	input := service.CreateAPIKeyInput{
		Namespace:   auth.Namespace,
		User:        auth.User,
		Team:        auth.Team,
		Description: derefString(req.Body.Description),
	}
	if req.Body.ExpiresAt != nil {
		if !req.Body.ExpiresAt.After(time.Now()) {
			return gen.SelfCreateAPIKey400JSONResponse{Error: "expiresAt must be in the future"}, nil
		}
		input.ExpiresAt = req.Body.ExpiresAt.UTC()
	}

	result, appErr := s.apikey.Create(ctx, input)
	if appErr != nil {
		return gen.SelfCreateAPIKey503JSONResponse(errResp(ctx, appErr)), nil
	}

	resp := gen.CreateAPIKeyResult{
		ApiKey:      result.RawToken,
		KeyId:       result.KeyID,
		User:        ptr.To(result.User),
		Team:        ptr.To(result.Team),
		Role:        result.Role,
		Description: ptr.To(result.Description),
		IssuedAt:    result.IssuedAt,
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = &result.ExpiresAt
	}
	return gen.SelfCreateAPIKey201JSONResponse(resp), nil
}

func (s *Server) SelfListAPIKeys(ctx context.Context, _ gen.SelfListAPIKeysRequestObject) (gen.SelfListAPIKeysResponseObject, error) {
	auth := authFrom(ctx)
	keys, appErr := s.apikey.ListByTeamAndUser(ctx, auth.Team, auth.User)
	if appErr != nil {
		return gen.SelfListAPIKeys503JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.SelfListAPIKeys200JSONResponse(apiKeyItemsToGen(keys)), nil
}

func (s *Server) SelfDeleteAPIKey(ctx context.Context, req gen.SelfDeleteAPIKeyRequestObject) (gen.SelfDeleteAPIKeyResponseObject, error) {
	auth := authFrom(ctx)

	// Fetch the key first to verify ownership.
	item, appErr := s.apikey.Get(ctx, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.SelfDeleteAPIKey404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.SelfDeleteAPIKey503JSONResponse(errResp(ctx, appErr)), nil
	}

	// Enforce: caller must own this key (same team + same user).
	if (auth.Team != "" && item.Team != auth.Team) || (auth.User != "" && item.User != auth.User) {
		return gen.SelfDeleteAPIKey403JSONResponse{Error: "forbidden: you can only delete your own api keys"}, nil
	}

	appErr = s.apikey.Delete(ctx, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.SelfDeleteAPIKey404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.SelfDeleteAPIKey503JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.SelfDeleteAPIKey200JSONResponse{KeyId: req.Name, Status: "Deleted"}, nil
}

// ---------------------------------------------------------------------------
// API Keys — Admin
// ---------------------------------------------------------------------------

func (s *Server) CreateAPIKey(ctx context.Context, req gen.CreateAPIKeyRequestObject) (gen.CreateAPIKeyResponseObject, error) {
	return s.createAPIKeyInternal(ctx, req.Body)
}

func (s *Server) createAPIKeyInternal(ctx context.Context, body *gen.CreateAPIKeyRequest) (gen.CreateAPIKeyResponseObject, error) {
	if body == nil {
		body = &gen.CreateAPIKeyRequest{}
	}

	auth := authFrom(ctx)
	namespace := derefString(body.Namespace)
	if namespace == "" {
		namespace = auth.Namespace
	}

	input := service.CreateAPIKeyInput{
		Namespace:   namespace,
		User:        derefString(body.User),
		Team:        derefString(body.Team),
		Description: derefString(body.Description),
		TokenHash:   derefString(body.TokenHash),
		HashPrefix:  derefString(body.HashPrefix),
	}
	if body.ExpiresAt != nil {
		if !body.ExpiresAt.After(time.Now()) {
			return gen.CreateAPIKey400JSONResponse{Error: "expiresAt must be in the future"}, nil
		}
		input.ExpiresAt = body.ExpiresAt.UTC()
	}
	if body.IssuedAt != nil {
		input.IssuedAt = body.IssuedAt.UTC()
	}

	result, appErr := s.apikey.Create(ctx, input)
	if appErr != nil {
		return gen.CreateAPIKey503JSONResponse(errResp(ctx, appErr)), nil
	}

	resp := gen.CreateAPIKeyResult{
		ApiKey:      result.RawToken,
		KeyId:       result.KeyID,
		User:        ptr.To(result.User),
		Team:        ptr.To(result.Team),
		Role:        result.Role,
		Description: ptr.To(result.Description),
		IssuedAt:    result.IssuedAt,
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = &result.ExpiresAt
	}
	return gen.CreateAPIKey201JSONResponse(resp), nil
}

func (s *Server) ListAPIKeys(ctx context.Context, req gen.ListAPIKeysRequestObject) (gen.ListAPIKeysResponseObject, error) {
	keys, appErr := s.apikey.List(ctx)
	if appErr != nil {
		return gen.ListAPIKeys503JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListAPIKeys200JSONResponse(apiKeyItemsToGen(keys)), nil
}

// apiKeyItemsToGen projects the service-layer APIKeyItem slice into the wire shape.
func apiKeyItemsToGen(items []service.APIKeyItem) []gen.APIKeyItem {
	out := make([]gen.APIKeyItem, 0, len(items))
	for _, item := range items {
		r := gen.APIKeyItem{
			KeyId:       item.ShortName,
			User:        ptr.To(item.User),
			Team:        ptr.To(item.Team),
			Role:        item.Role,
			Description: ptr.To(item.Description),
			IssuedAt:    item.IssuedAt,
			SyncSource:  ptr.To(item.SyncSource),
		}
		if !item.ExpiresAt.IsZero() {
			r.ExpiresAt = &item.ExpiresAt
		}
		if item.RawToken != "" {
			r.RawToken = ptr.To(item.RawToken)
		}
		out = append(out, r)
	}
	return out
}

func (s *Server) DeleteAPIKey(ctx context.Context, req gen.DeleteAPIKeyRequestObject) (gen.DeleteAPIKeyResponseObject, error) {
	appErr := s.apikey.Delete(ctx, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.DeleteAPIKey404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.DeleteAPIKey503JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.DeleteAPIKey200JSONResponse{KeyId: req.Name, Status: "Deleted"}, nil
}

func (s *Server) PromoteAPIKey(ctx context.Context, req gen.PromoteAPIKeyRequestObject) (gen.PromoteAPIKeyResponseObject, error) {
	appErr := s.apikey.Promote(ctx, req.Name)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.PromoteAPIKey404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.PromoteAPIKey409JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.PromoteAPIKey503JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.PromoteAPIKey200JSONResponse{KeyId: req.Name, Status: "Promoted"}, nil
}

// ---------------------------------------------------------------------------
// Organization
// ---------------------------------------------------------------------------

func (s *Server) ListTeams(ctx context.Context, _ gen.ListTeamsRequestObject) (gen.ListTeamsResponseObject, error) {
	teams, appErr := s.organization.ListTeams(ctx)
	if appErr != nil {
		return gen.ListTeams500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListTeams200JSONResponse{Items: teams, Total: len(teams)}, nil
}

func (s *Server) ListUsersByTeam(ctx context.Context, req gen.ListUsersByTeamRequestObject) (gen.ListUsersByTeamResponseObject, error) {
	users, appErr := s.organization.ListUsersByTeam(ctx, req.Team)
	if appErr != nil {
		return gen.ListUsersByTeam500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListUsersByTeam200JSONResponse{Items: users, Total: len(users)}, nil
}

func (s *Server) ListNamespaces(ctx context.Context, _ gen.ListNamespacesRequestObject) (gen.ListNamespacesResponseObject, error) {
	namespaces, appErr := s.organization.ListNamespaces(ctx)
	if appErr != nil {
		return gen.ListNamespaces500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListNamespaces200JSONResponse{Items: namespaces, Total: len(namespaces)}, nil
}

// ---------------------------------------------------------------------------
// Admin: Statistics
// ---------------------------------------------------------------------------

func (s *Server) AdminGetSandboxStatistics(ctx context.Context, _ gen.AdminGetSandboxStatisticsRequestObject) (gen.AdminGetSandboxStatisticsResponseObject, error) {
	result, appErr := s.sandbox.List(ctx, service.SandboxListFilter{Namespace: "", Limit: 0})
	if appErr != nil {
		return gen.AdminGetSandboxStatistics500JSONResponse(errResp(ctx, appErr)), nil
	}
	stats := gen.SandboxStatistics{
		Total:       result.Total,
		ByStatus:    make(map[string]int),
		ByNamespace: make(map[string]int),
	}
	for _, sb := range result.Items {
		stats.ByStatus[string(sb.Status)]++
		stats.ByNamespace[sb.Namespace]++
	}
	return gen.AdminGetSandboxStatistics200JSONResponse{Statistics: stats}, nil
}

// ---------------------------------------------------------------------------
// Statistics (user-scoped)
// ---------------------------------------------------------------------------

func (s *Server) GetUserSandboxStatistics(ctx context.Context, _ gen.GetUserSandboxStatisticsRequestObject) (gen.GetUserSandboxStatisticsResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.sandbox.List(ctx, service.SandboxListFilter{Namespace: auth.Namespace, Limit: 0})
	if appErr != nil {
		return gen.GetUserSandboxStatistics500JSONResponse(errResp(ctx, appErr)), nil
	}
	stats := gen.UserSandboxStatistics{
		Namespace: auth.Namespace,
		Total:     result.Total,
		ByStatus:  make(map[string]int),
	}
	for _, sb := range result.Items {
		stats.ByStatus[string(sb.Status)]++
	}
	return gen.GetUserSandboxStatistics200JSONResponse{Statistics: stats}, nil
}

func (s *Server) ExecSandboxCommand(ctx context.Context, req gen.ExecSandboxCommandRequestObject) (gen.ExecSandboxCommandResponseObject, error) {
	auth := authFrom(ctx)
	if req.Body == nil || strings.TrimSpace(req.Body.Command) == "" {
		return gen.ExecSandboxCommand400JSONResponse{Error: "command is required"}, nil
	}
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, jsonBody(req.Body))
		return nil, nil
	}
	result, appErr := s.sandbox.ExecCommand(ctx, auth.Namespace, req.SandboxId, *req.Body)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.ExecSandboxCommand404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeBadRequest:
			return gen.ExecSandboxCommand400JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.ExecSandboxCommand500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.ExecSandboxCommand200JSONResponse{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func (s *Server) IsSandboxReady(ctx context.Context, req gen.IsSandboxReadyRequestObject) (gen.IsSandboxReadyResponseObject, error) {
	auth := authFrom(ctx)
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}
	result, appErr := s.sandbox.IsReady(ctx, auth.Namespace, req.SandboxId)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.IsSandboxReady404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.IsSandboxReady500JSONResponse(errResp(ctx, appErr)), nil
	}
	return readinessToResponse(result), nil
}

// readinessToResponse picks the appropriate gen.IsSandboxReadyResponseObject
// based on the readiness result.
func readinessToResponse(result *gen.SandboxReadinessResult) gen.IsSandboxReadyResponseObject {
	if result.Ready {
		return gen.IsSandboxReady200JSONResponse(*result)
	}
	return gen.IsSandboxReady503JSONResponse(*result)
}

// ListClusters returns the gateway's cluster catalog so SDK/CLI callers can
// discover valid cross-cluster prefixes without reading private config.
func (s *Server) ListClusters(ctx context.Context, _ gen.ListClustersRequestObject) (gen.ListClustersResponseObject, error) {
	list, appErr := s.cluster.List(ctx)
	if appErr != nil {
		return gen.ListClusters500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListClusters200JSONResponse(gen.ListClustersResult{Clusters: list}), nil
}
