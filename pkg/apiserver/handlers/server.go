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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
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
	SandboxPool     service.SandboxPoolService
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
}

// Server implements gen.StrictServerInterface.
type Server struct {
	sandbox      service.SandboxService
	pool         service.SandboxPoolService
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
}

// NewServer creates a new handler Server.
func NewServer(svcs Services) *Server {
	qp := svcs.QuotaProvider
	if qp == nil {
		qp = quotaplugin.NewNoop()
	}
	return &Server{
		sandbox:       svcs.Sandbox,
		pool:          svcs.SandboxPool,
		template:      svcs.SandboxTemplate,
		apikey:        svcs.APIKey,
		quota:         svcs.Quota,
		organization:  svcs.Organization,
		sync:          svcs.Sync,
		forwarder:     svcs.Forwarder,
		cluster:       svcs.Cluster,
		quotaProvider: qp,
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

// sandboxToGen converts a domain.Sandbox to the generated gen.Sandbox type.
func sandboxToGen(s *domain.Sandbox) gen.Sandbox {
	sb := gen.Sandbox{
		SandboxId: s.SandboxID,
		Namespace: s.Namespace,
		PoolName:  s.PoolName,
		PodName:   s.PodName,
		Status:    gen.SandboxStatus(s.Status),
		Cpu:       ptr.To(s.CPU),
		Memory:    ptr.To(s.Memory),
	}
	if s.FailureReason != "" {
		sb.FailureReason = ptr.To(s.FailureReason)
	}
	if s.FailureMessage != "" {
		sb.FailureMessage = ptr.To(s.FailureMessage)
	}
	if s.ExitCode != nil {
		sb.ExitCode = s.ExitCode
	}
	if len(s.ContainerImages) > 0 {
		sb.ContainerImages = &s.ContainerImages
	}
	if len(s.Metadata) > 0 {
		sb.Metadata = &s.Metadata
	}
	if len(s.Endpoints) > 0 {
		eps := make(map[string]gen.SandboxEndpoint, len(s.Endpoints))
		for name, ep := range s.Endpoints {
			e := gen.SandboxEndpoint{Url: ep.URL}
			if ep.LogDir != "" {
				e.LogDir = &ep.LogDir
			}
			eps[name] = e
		}
		sb.Endpoints = &eps
	}
	if s.ClaimedAt != "" {
		if t, err := time.Parse(time.RFC3339, s.ClaimedAt); err == nil {
			sb.ClaimedAt = t
		}
	}
	if s.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, s.StartedAt); err == nil {
			sb.StartedAt = &t
		}
	}
	if s.TerminatedAt != "" {
		if t, err := time.Parse(time.RFC3339, s.TerminatedAt); err == nil {
			sb.TerminatedAt = &t
		}
	}
	if s.RecycledAt != "" {
		if t, err := time.Parse(time.RFC3339, s.RecycledAt); err == nil {
			sb.RecycledAt = &t
		}
	}
	if s.StatusDetail != nil {
		sd := gen.SandboxStatusDetail{
			Reason:  ptr.To(s.StatusDetail.Reason),
			Message: ptr.To(s.StatusDetail.Message),
		}
		if s.StatusDetail.LastUpdatedTime != "" {
			if t, err := time.Parse(time.RFC3339, s.StatusDetail.LastUpdatedTime); err == nil {
				sd.LastUpdatedTime = &t
			}
		}
		sb.StatusDetail = &sd
	}
	if s.Team != "" {
		sb.Team = &s.Team
	}
	if s.User != "" {
		sb.User = &s.User
	}
	if s.NodeName != "" {
		sb.NodeName = &s.NodeName
	}
	if s.ContainerID != "" {
		sb.ContainerId = &s.ContainerID
	}
	if s.DurationSeconds != nil {
		sb.DurationSeconds = s.DurationSeconds
	}
	return sb
}

// poolToGen converts a domain.SandboxPool to the generated gen.SandboxPool type.
func poolToGen(sp *domain.SandboxPool) gen.SandboxPool {
	spec := gen.SandboxPoolSpec{
		Replicas: sp.Spec.Replicas,
	}
	if sp.Spec.MinReplicas != nil {
		spec.MinReplicas = sp.Spec.MinReplicas
	}
	if sp.Spec.MaxReplicas != nil {
		spec.MaxReplicas = sp.Spec.MaxReplicas
	}
	if sp.Spec.TemplateName != "" {
		spec.TemplateName = ptr.To(sp.Spec.TemplateName)
	}
	if sp.Spec.Autoscaling != nil {
		spec.Autoscaling = autoscalingToGen(sp.Spec.Autoscaling)
	}
	if sp.Spec.DefaultStartupTimeout != nil {
		s := sp.Spec.DefaultStartupTimeout.Duration.String()
		spec.DefaultStartupTimeout = &s
	}
	if sp.Spec.DefaultIdleTimeout != nil {
		s := sp.Spec.DefaultIdleTimeout.Duration.String()
		spec.DefaultIdleTimeout = &s
	}

	status := gen.SandboxPoolStatus{
		IdleReplicas:            ptr.To(sp.Status.IdleReplicas),
		UnavailableIdleReplicas: ptr.To(sp.Status.UnavailableIdleReplicas),
		RunningReplicas:         ptr.To(sp.Status.RunningReplicas),
		StartingReplicas:        ptr.To(sp.Status.StartingReplicas),
		StoppingReplicas:        ptr.To(sp.Status.StoppingReplicas),
		FailedReplicas:          ptr.To(sp.Status.FailedReplicas),
	}
	if sp.Status.Phase != "" {
		phase := gen.SandboxPoolStatusPhase(sp.Status.Phase)
		status.Phase = &phase
	}

	specYaml := embeddedTemplateToYAML(sp.Spec.EmbeddedSandboxTemplate)
	result := gen.SandboxPool{
		Name:            sp.Name,
		Namespace:       sp.Namespace,
		Spec:            spec,
		Status:          status,
		Cpu:             ptr.To(sp.CPU),
		Memory:          ptr.To(sp.Memory),
		Team:            ptr.To(sp.Team),
		User:            ptr.To(sp.User),
		TemplateVersion: ptr.To(sp.TemplateVersion),
		SpecYaml:        ptr.To(specYaml),
		PoolDocs:        ptr.To(sp.PoolDocs),
	}
	if sp.Overrides != nil {
		result.Overrides = &gen.PoolTemplateOverrides{}
		if sp.Overrides.Image != "" {
			result.Overrides.Image = &sp.Overrides.Image
		}
		if sp.Overrides.ResourceMultiplier > 1 {
			result.Overrides.ResourceMultiplier = &sp.Overrides.ResourceMultiplier
		}
	}
	if sp.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, sp.CreatedAt); err == nil {
			result.CreatedAt = &t
		}
	}
	return result
}

// autoscalingToGen converts a CRD PoolAutoscalingSpec to the generated gen type.
func autoscalingToGen(a *agentsv1alpha1.PoolAutoscalingSpec) *gen.PoolAutoscalingSpec {
	if a == nil {
		return nil
	}
	result := &gen.PoolAutoscalingSpec{
		Enabled: &a.Enabled,
	}
	if a.ScaleUpPolicy != nil {
		mode := gen.PoolScaleUpPolicyMode(a.ScaleUpPolicy.Mode)
		result.ScaleUpPolicy = &gen.PoolScaleUpPolicy{
			Mode:                 &mode,
			CooldownSeconds:      &a.ScaleUpPolicy.CooldownSeconds,
			IdleThresholdSeconds: &a.ScaleUpPolicy.IdleThresholdSeconds,
		}
	}
	if a.ScaleDownPolicy != nil {
		result.ScaleDownPolicy = &gen.PoolScaleDownPolicy{
			IdleTimeoutSeconds:      &a.ScaleDownPolicy.IdleTimeoutSeconds,
			StabilizationSeconds:    &a.ScaleDownPolicy.StabilizationSeconds,
			ProtectionWindowSeconds: &a.ScaleDownPolicy.ProtectionWindowSeconds,
		}
	}
	return result
}

// genToAutoscaling converts a generated gen.PoolAutoscalingSpec to the CRD type.
func genToAutoscaling(a *gen.PoolAutoscalingSpec) *agentsv1alpha1.PoolAutoscalingSpec {
	if a == nil {
		return nil
	}
	result := &agentsv1alpha1.PoolAutoscalingSpec{}
	if a.Enabled != nil {
		result.Enabled = *a.Enabled
	}
	if a.ScaleUpPolicy != nil {
		result.ScaleUpPolicy = &agentsv1alpha1.PoolScaleUpPolicy{}
		if a.ScaleUpPolicy.Mode != nil {
			result.ScaleUpPolicy.Mode = agentsv1alpha1.PoolScaleUpMode(*a.ScaleUpPolicy.Mode)
		}
		if a.ScaleUpPolicy.CooldownSeconds != nil {
			result.ScaleUpPolicy.CooldownSeconds = *a.ScaleUpPolicy.CooldownSeconds
		}
		if a.ScaleUpPolicy.IdleThresholdSeconds != nil {
			result.ScaleUpPolicy.IdleThresholdSeconds = *a.ScaleUpPolicy.IdleThresholdSeconds
		}
	}
	if a.ScaleDownPolicy != nil {
		result.ScaleDownPolicy = &agentsv1alpha1.PoolScaleDownPolicy{}
		if a.ScaleDownPolicy.IdleTimeoutSeconds != nil {
			result.ScaleDownPolicy.IdleTimeoutSeconds = *a.ScaleDownPolicy.IdleTimeoutSeconds
		}
		if a.ScaleDownPolicy.StabilizationSeconds != nil {
			result.ScaleDownPolicy.StabilizationSeconds = *a.ScaleDownPolicy.StabilizationSeconds
		}
		if a.ScaleDownPolicy.ProtectionWindowSeconds != nil {
			result.ScaleDownPolicy.ProtectionWindowSeconds = *a.ScaleDownPolicy.ProtectionWindowSeconds
		}
	}
	return result
}

// embeddedTemplateToYAML serializes the EmbeddedSandboxTemplate fields (idleImage, runtimes,
// reservation, template) to a YAML string for use in the SyncTemplate diff view.
// Returns an empty string if marshalling fails.
func embeddedTemplateToYAML(emb agentsv1alpha1.EmbeddedSandboxTemplate) string {
	type diffable struct {
		IdleImage string                              `json:"idleImage,omitempty"`
		Runtimes  []agentsv1alpha1.SandboxRuntimeSpec `json:"runtimes,omitempty"`
		Template  *corev1.PodTemplateSpec             `json:"template,omitempty"`
	}
	d := diffable{
		IdleImage: emb.IdleImage,
		Runtimes:  emb.Runtimes,
		Template:  emb.Template,
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

// templateToGen converts a domain.SandboxTemplate to the generated gen.SandboxTemplate type.
func templateToGen(t *domain.SandboxTemplate) gen.SandboxTemplate {
	return service.TemplateToGen(t)
}

// templateToSummaryGen converts a domain.SandboxTemplate to the lightweight gen.SandboxTemplateSummary type.
func templateToSummaryGen(t *domain.SandboxTemplate) gen.SandboxTemplateSummary {
	return service.TemplateToSummaryGen(t)
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

// toCreatePoolInput converts a gen.CreateSandboxPoolRequest to a domain.CreateSandboxPoolInput.
// Returns an error if any duration field contains an invalid value.
func toCreatePoolInput(req gen.CreateSandboxPoolRequest, auth domain.AuthInfo) (domain.CreateSandboxPoolInput, error) { //nolint:gocyclo
	spec := agentsv1alpha1.SandboxPoolSpec{}
	if req.Spec != nil {
		spec = agentsv1alpha1.SandboxPoolSpec{
			Replicas:    req.Spec.Replicas,
			MinReplicas: req.Spec.MinReplicas,
			MaxReplicas: req.Spec.MaxReplicas,
		}
		if req.Spec.TemplateName != nil {
			spec.TemplateName = *req.Spec.TemplateName
		}
		if req.Spec.Autoscaling != nil {
			spec.Autoscaling = genToAutoscaling(req.Spec.Autoscaling)
		}
		if req.Spec.PodCreationImagePolicy != nil {
			spec.PodCreationImagePolicy = agentsv1alpha1.PodCreationImagePolicy(*req.Spec.PodCreationImagePolicy)
		}
		if req.Spec.DefaultStartupTimeout != nil && *req.Spec.DefaultStartupTimeout != "" {
			d, err := time.ParseDuration(*req.Spec.DefaultStartupTimeout)
			if err != nil || d <= 0 {
				return domain.CreateSandboxPoolInput{}, fmt.Errorf("defaultStartupTimeout must be a positive duration string (e.g. '2m')")
			}
			spec.DefaultStartupTimeout = &metav1.Duration{Duration: d}
		}
		if req.Spec.DefaultIdleTimeout != nil && *req.Spec.DefaultIdleTimeout != "" {
			d, err := time.ParseDuration(*req.Spec.DefaultIdleTimeout)
			if err != nil || d <= 0 {
				return domain.CreateSandboxPoolInput{}, fmt.Errorf("defaultIdleTimeout must be a positive duration string (e.g. '30m')")
			}
			spec.DefaultIdleTimeout = &metav1.Duration{Duration: d}
		}
	}
	// Also support top-level replicas/templateName fields
	if req.Replicas != nil {
		spec.Replicas = *req.Replicas
	}
	if req.MinReplicas != nil {
		spec.MinReplicas = req.MinReplicas
	}
	if req.MaxReplicas != nil {
		spec.MaxReplicas = req.MaxReplicas
	}

	templateName := ""
	if req.TemplateName != nil {
		templateName = *req.TemplateName
	} else if req.Spec != nil && req.Spec.TemplateName != nil {
		templateName = *req.Spec.TemplateName
	}
	if templateName != "" {
		spec.TemplateName = templateName
	}

	input := domain.CreateSandboxPoolInput{
		Name:         req.Name,
		Namespace:    auth.Namespace,
		TemplateName: templateName,
		Spec:         spec,
		Team:         auth.Team,
		User:         auth.User,
	}
	if req.Labels != nil {
		input.Labels = *req.Labels
	}
	if req.Annotations != nil {
		input.Annotations = *req.Annotations
	}
	if req.Overrides != nil {
		ov := &domain.PoolTemplateOverrides{}
		if req.Overrides.Image != nil {
			ov.Image = *req.Overrides.Image
		}
		if req.Overrides.ResourceMultiplier != nil {
			ov.ResourceMultiplier = *req.Overrides.ResourceMultiplier
		}
		// Only attach if at least one override is non-trivial.
		// resourceMultiplier==1 and image=="" are no-ops; skip them so the service
		// never sees an Overrides pointer for purely default values.
		if ov.Image != "" || ov.ResourceMultiplier > 1 {
			input.Overrides = ov
		}
	}
	if req.ImagePullSecret != nil && len(req.ImagePullSecret.Registries) > 0 {
		regs := make([]domain.RegistryCredential, 0, len(req.ImagePullSecret.Registries))
		for i, r := range req.ImagePullSecret.Registries {
			if strings.TrimSpace(r.Registry) == "" {
				return domain.CreateSandboxPoolInput{}, fmt.Errorf("imagePullSecret.registries[%d].registry is required", i)
			}
			if r.Username == "" {
				return domain.CreateSandboxPoolInput{}, fmt.Errorf("imagePullSecret.registries[%d].username is required", i)
			}
			if r.Password == "" {
				return domain.CreateSandboxPoolInput{}, fmt.Errorf("imagePullSecret.registries[%d].password is required", i)
			}
			regs = append(regs, domain.RegistryCredential{
				Registry: strings.TrimSpace(r.Registry),
				Username: r.Username,
				Password: r.Password,
			})
		}
		input.ImagePullSecret = &domain.ImagePullSecretInput{Registries: regs}
	}
	return input, nil
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

	input := domain.CreateSandboxInput{
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
	return gen.CreateSandbox201JSONResponse{Sandbox: sandboxToGen(result)}, nil
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
	filter := domain.ListSandboxesFilter{
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

	items := make([]gen.Sandbox, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, sandboxToGen(&result.Items[i]))
	}
	return gen.ListSandboxes200JSONResponse{
		Items:  items,
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
	return gen.GetSandbox200JSONResponse{Sandbox: sandboxToGen(result)}, nil
}

func (s *Server) GetSandboxLogs(ctx context.Context, req gen.GetSandboxLogsRequestObject) (gen.GetSandboxLogsResponseObject, error) {
	auth := authFrom(ctx)
	opts := domain.GetLogsOptions{}
	if req.Params.Container != nil {
		opts.Container = *req.Params.Container
	}
	if req.Params.Lines != nil {
		opts.Lines = *req.Params.Lines
	}
	if req.Params.Source != nil {
		opts.Source = *req.Params.Source
	}

	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, nil)
		return nil, nil
	}

	result, appErr := s.sandbox.GetLogs(ctx, auth.Namespace, req.SandboxId, opts)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandboxLogs404JSONResponse(errResp(ctx, appErr)), nil
		}
		if appErr.Code == domain.ErrCodeBadRequest {
			return gen.GetSandboxLogs400JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandboxLogs500JSONResponse(errResp(ctx, appErr)), nil
	}

	entries := make([]gen.SandboxLogEntry, 0, len(result.Entries))
	for _, e := range result.Entries {
		entry := gen.SandboxLogEntry{
			Container: e.Container,
			Log:       e.Log,
		}
		if !e.Timestamp.IsZero() {
			t := e.Timestamp
			entry.Timestamp = &t
		}
		entries = append(entries, entry)
	}

	resp := gen.SandboxLogsResult{
		SandboxId: result.SandboxID,
		Namespace: result.Namespace,
		PodName:   ptr.To(result.PodName),
		Entries:   entries,
		Truncated: false,
		Source:    gen.SandboxLogsResultSource(result.Source),
	}
	if result.TotalBytes > 0 {
		resp.TotalBytes = &result.TotalBytes
	}
	if result.RuntimeName != "" {
		resp.RuntimeName = &result.RuntimeName
	}

	return gen.GetSandboxLogs200JSONResponse(resp), nil
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
		SandboxId: result.SandboxID,
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

func (s *Server) CreateSandboxPool(ctx context.Context, req gen.CreateSandboxPoolRequestObject) (gen.CreateSandboxPoolResponseObject, error) {
	if req.Body == nil || strings.TrimSpace(req.Body.Name) == "" {
		return gen.CreateSandboxPool400JSONResponse{Error: "name is required"}, nil
	}
	if err := k8sname.Validate(req.Body.Name); err != nil {
		return gen.CreateSandboxPool400JSONResponse{Error: err.Error()}, nil
	}

	auth := authFrom(ctx)
	input, err := toCreatePoolInput(*req.Body, auth)
	if err != nil {
		return gen.CreateSandboxPool400JSONResponse{Error: err.Error()}, nil
	}

	// Require the acting user to have at least one API Key before creating a Pool.
	// This ensures every Pool owner is visible in the admin Teams/Users interfaces,
	// which are now backed by API Key secrets rather than ScitixQuota CRDs.
	//
	// Skip only when the caller is a pure admin (User="admin"), not when impersonating:
	// impersonation replaces auth.User with the target user, so the check naturally
	// applies to the impersonated identity — enforcing data consistency across the board.
	if auth.Role != apikey.RoleAdmin || auth.User != adminUser {
		keys, apiKeyErr := s.apikey.ListByTeamAndUser(ctx, auth.Team, auth.User)
		if apiKeyErr != nil {
			return gen.CreateSandboxPool500JSONResponse(errResp(ctx, apiKeyErr)), nil
		}
		if keys == nil || len(keys.Items) == 0 {
			apiKeyAppErr := domain.NewAPIKeyRequired(
				"no API key found for this user; please create one on the API Keys page before creating a SandboxPool",
			)
			return gen.CreateSandboxPool422JSONResponse(errResp(ctx, apiKeyAppErr)), nil
		}
	}

	result, appErr := s.pool.Create(ctx, input)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.CreateSandboxPool400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			// Template not found — return as 400 with availableTemplates
			// discovery hint. (404 on a write endpoint is confusing because the
			// caller is not reading; we keep the status as 400 and convey the
			// missing-resource signal via the error message + detail.)
			return gen.CreateSandboxPool400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeConflict:
			return gen.CreateSandboxPool409JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeTooManyRequests:
			return gen.CreateSandboxPool429JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.CreateSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.CreateSandboxPool201JSONResponse{Template: poolToGen(result)}, nil
}

func (s *Server) ListSandboxPools(ctx context.Context, _ gen.ListSandboxPoolsRequestObject) (gen.ListSandboxPoolsResponseObject, error) {
	auth := authFrom(ctx)
	items, appErr := s.pool.List(ctx, auth.Namespace, auth.Team, auth.User)
	if appErr != nil {
		return gen.ListSandboxPools500JSONResponse(errResp(ctx, appErr)), nil
	}
	payloads := make([]gen.SandboxPool, 0, len(items))
	for i := range items {
		payloads = append(payloads, poolToGen(&items[i]))
	}
	return gen.ListSandboxPools200JSONResponse{
		Items:  payloads,
		Total:  len(payloads),
		Limit:  0,
		Offset: 0,
	}, nil
}

func (s *Server) GetSandboxPool(ctx context.Context, req gen.GetSandboxPoolRequestObject) (gen.GetSandboxPoolResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.pool.Get(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.GetSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.GetSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
	}

	// Render pool docs template on the server so the client gets a ready-to-copy
	// snippet. When the raw docs reference ${apiKey} and the user has no key
	// with a recoverable plaintext token, return API_KEY_REQUIRED (422) so the
	// frontend can guide them to the API Keys page.
	rendered, renderErr := s.renderPoolDocs(ctx, result.PoolDocs, result.Name, s.forwarder.LocalClusterID(), auth)
	if renderErr != nil {
		switch renderErr.Code {
		case domain.ErrCodeUnprocessableEntity:
			return gen.GetSandboxPool422JSONResponse(errResp(ctx, renderErr)), nil
		default:
			return gen.GetSandboxPool500JSONResponse(errResp(ctx, renderErr)), nil
		}
	}
	result.PoolDocs = rendered

	return gen.GetSandboxPool200JSONResponse{Template: poolToGen(result)}, nil
}

func (s *Server) UpdateSandboxPool(ctx context.Context, req gen.UpdateSandboxPoolRequestObject) (gen.UpdateSandboxPoolResponseObject, error) {
	if req.Body == nil {
		return gen.UpdateSandboxPool400JSONResponse{Error: "request body is required"}, nil
	}
	hasReplicas := req.Body.Replicas != nil || req.Body.MinReplicas != nil || req.Body.MaxReplicas != nil
	hasImage := req.Body.Overrides != nil && req.Body.Overrides.Image != nil && *req.Body.Overrides.Image != ""
	hasAutoscaling := req.Body.Autoscaling != nil
	hasPodCreationImagePolicy := req.Body.PodCreationImagePolicy != nil
	if !hasReplicas && !hasImage && !hasAutoscaling && !hasPodCreationImagePolicy {
		return gen.UpdateSandboxPool400JSONResponse{Error: "at least one of replicas, minReplicas, maxReplicas, overrides.image, autoscaling, or podCreationImagePolicy is required"}, nil
	}
	auth := authFrom(ctx)
	input := domain.UpdateSandboxPoolInput{
		Name:        req.Name,
		Namespace:   auth.Namespace,
		Replicas:    req.Body.Replicas,
		MinReplicas: req.Body.MinReplicas,
		MaxReplicas: req.Body.MaxReplicas,
	}
	if hasImage {
		input.OverrideImage = *req.Body.Overrides.Image
	}
	if hasAutoscaling {
		input.Autoscaling = genToAutoscaling(req.Body.Autoscaling)
	}
	if hasPodCreationImagePolicy {
		pol := agentsv1alpha1.PodCreationImagePolicy(*req.Body.PodCreationImagePolicy)
		input.PodCreationImagePolicy = &pol
	}
	result, appErr := s.pool.Update(ctx, input)
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeBadRequest:
			return gen.UpdateSandboxPool400JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeNotFound:
			return gen.UpdateSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeTooManyRequests:
			return gen.UpdateSandboxPool429JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.UpdateSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.UpdateSandboxPool200JSONResponse{Template: poolToGen(result)}, nil
}

func (s *Server) DeleteSandboxPool(ctx context.Context, req gen.DeleteSandboxPoolRequestObject) (gen.DeleteSandboxPoolResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.pool.Delete(ctx, auth.Namespace, req.Name)
	if appErr != nil {
		if appErr.Code == domain.ErrCodeNotFound {
			return gen.DeleteSandboxPool404JSONResponse(errResp(ctx, appErr)), nil
		}
		return gen.DeleteSandboxPool500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.DeleteSandboxPool202JSONResponse{
		Name:      result.Name,
		Namespace: result.Namespace,
		Status:    "Terminating",
	}, nil
}

func (s *Server) SyncSandboxPoolTemplate(ctx context.Context, req gen.SyncSandboxPoolTemplateRequestObject) (gen.SyncSandboxPoolTemplateResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.pool.SyncTemplate(ctx, domain.SyncSandboxPoolTemplateInput{
		Name:      req.Name,
		Namespace: auth.Namespace,
	})
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.SyncSandboxPoolTemplate404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeBadRequest:
			return gen.SyncSandboxPoolTemplate400JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.SyncSandboxPoolTemplate500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.SyncSandboxPoolTemplate200JSONResponse{Template: poolToGen(result)}, nil
}

func (s *Server) PreviewSyncSandboxPoolTemplate(ctx context.Context, req gen.PreviewSyncSandboxPoolTemplateRequestObject) (gen.PreviewSyncSandboxPoolTemplateResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.pool.SyncTemplatePreview(ctx, domain.SyncSandboxPoolTemplateInput{
		Name:      req.Name,
		Namespace: auth.Namespace,
	})
	if appErr != nil {
		switch appErr.Code {
		case domain.ErrCodeNotFound:
			return gen.PreviewSyncSandboxPoolTemplate404JSONResponse(errResp(ctx, appErr)), nil
		case domain.ErrCodeBadRequest:
			return gen.PreviewSyncSandboxPoolTemplate400JSONResponse(errResp(ctx, appErr)), nil
		default:
			return gen.PreviewSyncSandboxPoolTemplate500JSONResponse(errResp(ctx, appErr)), nil
		}
	}
	return gen.PreviewSyncSandboxPoolTemplate200JSONResponse{
		SpecYaml: result.SpecYaml,
		Version:  result.Version,
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
		payloads = append(payloads, templateToSummaryGen(&items[i]))
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
	result.Docs = renderTemplateDocs(result.Docs, s.forwarder.LocalClusterID())
	return gen.GetSandboxTemplate200JSONResponse{Template: templateToGen(result)}, nil
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
		return gen.AdminCreateSandboxTemplate201JSONResponse{Template: templateToGen(result)}, nil
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
	return gen.AdminCreateSandboxTemplate201JSONResponse{Template: templateToGen(result)}, nil
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
		return gen.AdminUpdateSandboxTemplate200JSONResponse{Template: templateToGen(result)}, nil
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
	return gen.AdminUpdateSandboxTemplate200JSONResponse{Template: templateToGen(result)}, nil
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
		if tmpl.SyncSource == agentsv1alpha1.LabelSyncSourceGlobal {
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
		payloads = append(payloads, templateToSummaryGen(&items[i]))
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
	items := make([]gen.Quota, 0, len(quotas))
	for i := range quotas {
		items = append(items, domainQuotaToGen(&quotas[i]))
	}
	return gen.ListQuotas200JSONResponse{
		Items:  items,
		Total:  len(items),
		Limit:  0,
		Offset: 0,
	}, nil
}

func domainQuotaToGen(q *domain.QuotaInfo) gen.Quota {
	label := q.QuotaURL
	if label == "" {
		label = q.Name
	}
	quota := gen.Quota{
		Name:     q.Name,
		QuotaUrl: q.QuotaURL,
		Queue:    q.Queue,
		PoolName: ptr.To(q.PoolName),
		Label:    label,
		Team:     ptr.To(q.Team),
		User:     ptr.To(q.User),
	}
	if len(q.Resources) > 0 {
		quota.Resources = &q.Resources
	}
	if len(q.Used) > 0 {
		quota.Used = &q.Used
	}
	if len(q.Reserved) > 0 {
		quota.Reserved = &q.Reserved
	}
	if len(q.Free) > 0 {
		quota.Free = &q.Free
	}
	return quota
}

// ---------------------------------------------------------------------------
// API Keys — Self-service (tenant: namespace/user/team locked to auth context)
// ---------------------------------------------------------------------------

func (s *Server) SelfCreateAPIKey(ctx context.Context, req gen.SelfCreateAPIKeyRequestObject) (gen.SelfCreateAPIKeyResponseObject, error) {
	if req.Body == nil {
		req.Body = &gen.SelfCreateAPIKeyRequest{}
	}
	auth := authFrom(ctx)

	input := domain.CreateAPIKeyInput{
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
	result, appErr := s.apikey.ListByTeamAndUser(ctx, auth.Team, auth.User)
	if appErr != nil {
		return gen.SelfListAPIKeys503JSONResponse(errResp(ctx, appErr)), nil
	}

	items := make([]gen.APIKeyItem, 0, len(result.Items))
	for _, item := range result.Items {
		r := gen.APIKeyItem{
			KeyId:       item.ShortName,
			User:        ptr.To(item.User),
			Team:        ptr.To(item.Team),
			Role:        item.Role,
			QuotaURL:    ptr.To(item.QuotaURL),
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
		items = append(items, r)
	}
	return gen.SelfListAPIKeys200JSONResponse{
		Items:  items,
		Total:  len(items),
		Limit:  0,
		Offset: 0,
	}, nil
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

	appErr = s.apikey.Delete(ctx, domain.DeleteAPIKeyInput{KeyID: req.Name})
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

	input := domain.CreateAPIKeyInput{
		Namespace:   namespace,
		User:        derefString(body.User),
		Team:        derefString(body.Team),
		Description: derefString(body.Description),
		TokenHash:   derefString(body.TokenHash),
		HashPrefix:  derefString(body.HashPrefix),
		QuotaURL:    derefString(body.QuotaURL),
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
	result, appErr := s.apikey.List(ctx)
	if appErr != nil {
		return gen.ListAPIKeys503JSONResponse(errResp(ctx, appErr)), nil
	}
	items := make([]gen.APIKeyItem, 0, len(result.Items))
	for _, item := range result.Items {
		r := gen.APIKeyItem{
			KeyId:       item.ShortName,
			User:        ptr.To(item.User),
			Team:        ptr.To(item.Team),
			Role:        item.Role,
			QuotaURL:    ptr.To(item.QuotaURL),
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
		items = append(items, r)
	}
	return gen.ListAPIKeys200JSONResponse{
		Items:  items,
		Total:  len(items),
		Limit:  0,
		Offset: 0,
	}, nil
}

func (s *Server) DeleteAPIKey(ctx context.Context, req gen.DeleteAPIKeyRequestObject) (gen.DeleteAPIKeyResponseObject, error) {
	appErr := s.apikey.Delete(ctx, domain.DeleteAPIKeyInput{KeyID: req.Name})
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
	result, appErr := s.sandbox.List(ctx, domain.ListSandboxesFilter{Namespace: "", Limit: 0})
	if appErr != nil {
		return gen.AdminGetSandboxStatistics500JSONResponse(errResp(ctx, appErr)), nil
	}
	stats := gen.SandboxStatistics{
		Total:       result.Total,
		ByStatus:    make(map[string]int),
		ByNamespace: make(map[string]int),
	}
	for _, sb := range result.Items {
		stats.ByStatus[sb.Status]++
		stats.ByNamespace[sb.Namespace]++
	}
	return gen.AdminGetSandboxStatistics200JSONResponse{Statistics: stats}, nil
}

func (s *Server) AdminGetSandboxPoolStatistics(ctx context.Context, _ gen.AdminGetSandboxPoolStatisticsRequestObject) (gen.AdminGetSandboxPoolStatisticsResponseObject, error) {
	// Admin statistics: list all pools across all namespaces (empty team/user = no label filter)
	items, appErr := s.pool.List(ctx, "", "", "")
	if appErr != nil {
		return gen.AdminGetSandboxPoolStatistics500JSONResponse(errResp(ctx, appErr)), nil
	}
	stats := gen.SandboxPoolStatistics{
		Total:       len(items),
		ByNamespace: make(map[string]int),
	}
	for _, pool := range items {
		stats.ByNamespace[pool.Namespace]++
		stats.TotalReplicas += int(pool.Spec.Replicas)
		stats.TotalIdleReplicas += int(pool.Status.IdleReplicas)
		stats.TotalRunningReplicas += int(pool.Status.RunningReplicas)
		stats.TotalFailedReplicas += int(pool.Status.FailedReplicas)
	}
	return gen.AdminGetSandboxPoolStatistics200JSONResponse{Statistics: stats}, nil
}

// ---------------------------------------------------------------------------
// Statistics (user-scoped)
// ---------------------------------------------------------------------------

func (s *Server) GetUserSandboxStatistics(ctx context.Context, _ gen.GetUserSandboxStatisticsRequestObject) (gen.GetUserSandboxStatisticsResponseObject, error) {
	auth := authFrom(ctx)
	result, appErr := s.sandbox.List(ctx, domain.ListSandboxesFilter{Namespace: auth.Namespace, Limit: 0})
	if appErr != nil {
		return gen.GetUserSandboxStatistics500JSONResponse(errResp(ctx, appErr)), nil
	}
	stats := gen.UserSandboxStatistics{
		Namespace: auth.Namespace,
		Total:     result.Total,
		ByStatus:  make(map[string]int),
	}
	for _, sb := range result.Items {
		stats.ByStatus[sb.Status]++
	}
	return gen.GetUserSandboxStatistics200JSONResponse{Statistics: stats}, nil
}

func (s *Server) ExecSandboxCommand(ctx context.Context, req gen.ExecSandboxCommandRequestObject) (gen.ExecSandboxCommandResponseObject, error) {
	auth := authFrom(ctx)
	if req.Body == nil || strings.TrimSpace(req.Body.Command) == "" {
		return gen.ExecSandboxCommand400JSONResponse{Error: "command is required"}, nil
	}
	input := domain.ExecCommandInput{
		Command: req.Body.Command,
	}
	if req.Body.TimeoutSeconds != nil {
		input.TimeoutSeconds = *req.Body.TimeoutSeconds
	}
	// Cross-cluster forwarding.
	if cID, _ := cluster.SplitSandboxID(req.SandboxId); s.isCrossCluster(cID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), cID, service.URLKindNative, jsonBody(req.Body))
		return nil, nil
	}
	result, appErr := s.sandbox.ExecCommand(ctx, auth.Namespace, req.SandboxId, input)
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

// readinessToResponse converts a domain.SandboxReadinessResult to the appropriate
// gen.IsSandboxReadyResponseObject.
func readinessToResponse(result *domain.SandboxReadinessResult) gen.IsSandboxReadyResponseObject {
	eps := make(map[string]struct {
		Message *string `json:"message,omitempty"`
		Ready   *bool   `json:"ready,omitempty"`
	}, len(result.Endpoints))
	for name, ep := range result.Endpoints {
		ready := ep.Ready
		e := struct {
			Message *string `json:"message,omitempty"`
			Ready   *bool   `json:"ready,omitempty"`
		}{Ready: &ready}
		if ep.Message != "" {
			e.Message = &ep.Message
		}
		eps[name] = e
	}
	resp := gen.SandboxReadinessResult{
		SandboxId: result.SandboxID,
		Ready:     result.Ready,
		Endpoints: &eps,
	}
	if result.Ready {
		return gen.IsSandboxReady200JSONResponse(resp)
	}
	return gen.IsSandboxReady503JSONResponse(resp)
}

// ListClusters returns the gateway's cluster catalog so SDK/CLI callers can
// discover valid cross-cluster prefixes without reading private config.
func (s *Server) ListClusters(ctx context.Context, _ gen.ListClustersRequestObject) (gen.ListClustersResponseObject, error) {
	list, appErr := s.cluster.List(ctx)
	if appErr != nil {
		return gen.ListClusters500JSONResponse(errResp(ctx, appErr)), nil
	}
	items := make([]gen.ClusterSummary, 0, len(list))
	for _, c := range list {
		items = append(items, gen.ClusterSummary{
			Id:    c.ID,
			Name:  ptr.To(c.Name),
			Local: c.Local,
		})
	}
	return gen.ListClusters200JSONResponse(gen.ListClustersResult{Clusters: items}), nil
}
