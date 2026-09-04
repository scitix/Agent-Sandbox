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
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
)

// Sandbox metrics, read from the cluster's own metrics backend.
//
// A sandbox is a container, so the numbers come from the container metrics
// already being collected (cAdvisor series), scoped to the Pod backing the
// sandbox. Nothing new is scraped and nothing is stored here.
//
// When no backend is configured the endpoints answer 501 naming that fact,
// rather than an empty array. An empty array is indistinguishable from "this
// sandbox is doing nothing", which is a worse answer than "I cannot tell you".

const (
	// metricsRangePoints is the number of samples a range query aims for. The
	// step is derived from it so a one-hour and a one-day window both come back
	// at a size a caller can actually use.
	metricsRangePoints = 120
	metricsMinStep     = 15 * time.Second
	metricsMaxStep     = 5 * time.Minute
	// metricsDefaultWindow is used when the caller gives neither start nor end.
	metricsDefaultWindow = 15 * time.Minute
)

// metricsUnavailable reports that this deployment has no metrics backend.
func (s *Server) metricsUnavailable(op string) unsupported {
	return unsupportedOp(op, catUnimplemented, msgMetrics)
}

func (s *Server) metricsReady() bool {
	return s.metrics != nil && s.metrics.Ready()
}

// podSelector builds the label matcher identifying one sandbox's container.
// clusterSelector is the per-cluster matcher from the cluster config; it is
// what keeps a shared, multi-cluster backend from answering with another
// cluster's identically-named pod.
func (s *Server) podSelector(namespace, podName string) string {
	parts := []string{
		fmt.Sprintf("namespace=%q", namespace),
		fmt.Sprintf("pod=%q", podName),
	}
	// Without the cluster matcher a shared backend can answer with another
	// cluster's identically-named pod, which is worse than no answer.
	if s.metricsSelector != nil {
		if sel := strings.TrimSpace(s.metricsSelector()); sel != "" {
			parts = append(parts, sel)
		}
	}
	return strings.Join(parts, ",")
}

// sandboxMetricAt collects one point in time for a sandbox.
func (s *Server) sandboxMetricAt(ctx context.Context, sb *gen.Sandbox, namespace string, at time.Time) e2bgen.SandboxMetric {
	sel := s.podSelector(namespace, sb.PodName)

	// Cpu and Memory are Kubernetes resource quantities ("1", "500m", "16Gi"),
	// not numbers, so they are parsed rather than cast.
	cpuCount := int32(0)
	if sb.Cpu != nil {
		if q, err := resource.ParseQuantity(*sb.Cpu); err == nil {
			cpuCount = int32(math.Ceil(q.AsApproximateFloat64()))
		}
	}
	memTotal := int64(0)
	if sb.Memory != nil {
		if q, err := resource.ParseQuantity(*sb.Memory); err == nil {
			memTotal = q.Value()
		}
	}

	metric := e2bgen.SandboxMetric{
		Timestamp:     at,
		TimestampUnix: at.Unix(),
		CpuCount:      cpuCount,
		MemTotal:      memTotal,
	}

	// One query per quantity rather than one combined query: the series have
	// different names and any of them may be missing on a given cluster, and a
	// combined query would fail as a whole when one is.
	if v, ok := s.scalarAt(ctx, fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{%s,container!="",container!="POD"}[2m]))`, sel), at); ok {
		if cpuCount > 0 {
			metric.CpuUsedPct = float32(v / float64(cpuCount) * 100)
		} else {
			metric.CpuUsedPct = float32(v * 100)
		}
	}
	if v, ok := s.scalarAt(ctx, fmt.Sprintf(
		`sum(container_memory_working_set_bytes{%s,container!="",container!="POD"})`, sel), at); ok {
		metric.MemUsed = int64(v)
	}
	if v, ok := s.scalarAt(ctx, fmt.Sprintf(
		`sum(container_memory_cache{%s,container!="",container!="POD"})`, sel), at); ok {
		metric.MemCache = int64(v)
	}
	if v, ok := s.scalarAt(ctx, fmt.Sprintf(`sum(container_fs_usage_bytes{%s})`, sel), at); ok {
		metric.DiskUsed = int64(v)
	}
	if v, ok := s.scalarAt(ctx, fmt.Sprintf(`max(container_fs_limit_bytes{%s})`, sel), at); ok {
		metric.DiskTotal = int64(v)
	}
	return metric
}

// scalarAt runs an instant query and returns the first value, if any. A missing
// series is not an error: clusters differ in which cAdvisor series they expose,
// and one absent quantity should leave that field zero rather than fail the
// whole response.
func (s *Server) scalarAt(ctx context.Context, promql string, at time.Time) (float64, bool) {
	series, err := s.metrics.Query(ctx, promql, at)
	if err != nil || len(series) == 0 || len(series[0].Samples) == 0 {
		return 0, false
	}
	return series[0].Samples[0].Value, true
}

func (s *Server) GetSandboxesSandboxIDMetrics(ctx context.Context, req e2bgen.GetSandboxesSandboxIDMetricsRequestObject) (e2bgen.GetSandboxesSandboxIDMetricsResponseObject, error) {
	if clusterID, _ := cluster.SplitSandboxID(req.SandboxID); s.isCrossCluster(clusterID) {
		// Metrics are read from the owning cluster's own backend selector, so
		// the request follows the sandbox rather than being answered here.
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), clusterID, service.URLKindE2B, nil)
		return nil, nil
	}
	if !s.metricsReady() {
		return s.metricsUnavailable("GetSandboxesSandboxIDMetrics"), nil
	}

	auth := authFrom(ctx)
	sb, appErr := s.sandbox.GetLive(ctx, auth.Namespace, req.SandboxID)
	if appErr != nil {
		if appErr.Code == apidomain.ErrCodeNotFound {
			return e2bgen.GetSandboxesSandboxIDMetrics404JSONResponse{
				N404JSONResponse: e2bgen.N404JSONResponse(errRespCode(404, appErr.Message))}, nil
		}
		return e2bgen.GetSandboxesSandboxIDMetrics500JSONResponse{
			N500JSONResponse: e2bgen.N500JSONResponse(errRespAppErr(ctx, appErr))}, nil
	}

	end := time.Now()
	if req.Params.End != nil && *req.Params.End > 0 {
		end = time.Unix(*req.Params.End, 0)
	}
	start := end.Add(-metricsDefaultWindow)
	if req.Params.Start != nil && *req.Params.Start > 0 {
		start = time.Unix(*req.Params.Start, 0)
	}
	if !start.Before(end) {
		return e2bgen.GetSandboxesSandboxIDMetrics400JSONResponse{
			N400JSONResponse: e2bgen.N400JSONResponse(errRespCode(400,
				"start must be earlier than end"))}, nil
	}

	// A query failure for one quantity leaves that field zero rather than
	// failing the response; see scalarAt.
	points := s.rangeMetrics(ctx, sb, auth.Namespace, start, end)
	return e2bgen.GetSandboxesSandboxIDMetrics200JSONResponse(points), nil
}

// rangeMetrics samples the window at a step chosen from its length.
func (s *Server) rangeMetrics(ctx context.Context, sb *gen.Sandbox, namespace string, start, end time.Time) []e2bgen.SandboxMetric {
	step := end.Sub(start) / metricsRangePoints
	step = min(max(step, metricsMinStep), metricsMaxStep)

	out := make([]e2bgen.SandboxMetric, 0, metricsRangePoints)
	for at := start; !at.After(end); at = at.Add(step) {
		out = append(out, s.sandboxMetricAt(ctx, sb, namespace, at))
	}
	return out
}

func (s *Server) GetSandboxesMetrics(ctx context.Context, req e2bgen.GetSandboxesMetricsRequestObject) (e2bgen.GetSandboxesMetricsResponseObject, error) {
	if !s.metricsReady() {
		return s.metricsUnavailable("GetSandboxesMetrics"), nil
	}
	auth := authFrom(ctx)

	ids := req.Params.SandboxIds
	sort.Strings(ids)

	now := time.Now()
	out := e2bgen.SandboxesWithMetrics{Sandboxes: map[string]e2bgen.SandboxMetric{}}
	for _, id := range ids {
		// Upstream reports only the sandboxes that are running, so an id that
		// no longer resolves is skipped rather than failing the whole batch —
		// a caller polling a set of sandboxes should not lose the readings for
		// the live ones because one has ended.
		sb, appErr := s.sandbox.GetLive(ctx, auth.Namespace, id)
		if appErr != nil {
			continue
		}
		out.Sandboxes[id] = s.sandboxMetricAt(ctx, sb, auth.Namespace, now)
	}
	return e2bgen.GetSandboxesMetrics200JSONResponse(out), nil
}
