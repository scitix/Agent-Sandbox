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

package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// agentboxNamespace is the namespace sandbox pods run in on each worker
// cluster, used to scope Envoy proxy metrics. Overridable for deployments
// that use a non-default namespace, matching the dashboard's own
// prometheus-report-core.ts convention.
var agentboxNamespace = envOrDefault("AGENTBOX_NAMESPACE", "agentbox-system")

const (
	envoyClusterName    = "original_dst_cluster"
	defaultSubqueryStep = "1m"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Scalar is a nullable metric value. nil means "n/a" — either the query
// legitimately returned no data, or (in the idle-check path) the query
// failed and the result must be treated as indeterminate.
type Scalar = *float64

func f64(v float64) Scalar { return &v }

func divide(num, den Scalar) Scalar {
	if num == nil || den == nil || *den == 0 {
		return nil
	}
	v := *num / *den
	return &v
}

func sumScalars(vals ...Scalar) Scalar {
	total := 0.0
	for _, v := range vals {
		if v != nil {
			total += *v
		}
	}
	return &total
}

// ── HTTP client ─────────────────────────────────────────────────────────────

type vectorSample struct {
	Labels map[string]string
	Value  float64
}

type promClient struct {
	baseURL string
	token   string
	hc      *http.Client
}

func newPromClient(baseURL, token string) *promClient {
	return &promClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

type promQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

// promStatusSuccess is the Prometheus HTTP API's own "status" field value. It
// coincides with ResultSuccess textually but is a separate protocol constant —
// one describes an upstream query, the other a notification send.
const promStatusSuccess = "success"

func (p *promClient) doGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// query runs an instant query and returns the first sample's scalar value,
// or nil if the result vector is empty. at is optional (zero value = now).
func (p *promClient) query(ctx context.Context, promql string, at time.Time) (Scalar, error) {
	params := url.Values{}
	params.Set("query", promql)
	if !at.IsZero() {
		params.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	body, err := p.doGet(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, err
	}
	var payload promQueryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if payload.Status != promStatusSuccess {
		return nil, fmt.Errorf("prometheus query failed: %s", payload.Error)
	}
	if len(payload.Data.Result) == 0 || len(payload.Data.Result[0].Value) < 2 {
		return nil, nil
	}
	return parseScalarValue(payload.Data.Result[0].Value[1])
}

// queryVector runs an instant query and returns every series in the result
// vector (label set + value), for grouped queries like `sum by (team,user)`.
func (p *promClient) queryVector(ctx context.Context, promql string, at time.Time) ([]vectorSample, error) {
	params := url.Values{}
	params.Set("query", promql)
	if !at.IsZero() {
		params.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	body, err := p.doGet(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, err
	}
	var payload promQueryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if payload.Status != promStatusSuccess {
		return nil, fmt.Errorf("prometheus query failed: %s", payload.Error)
	}
	samples := make([]vectorSample, 0, len(payload.Data.Result))
	for _, r := range payload.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		v, err := parseScalarValue(r.Value[1])
		if err != nil || v == nil {
			continue
		}
		samples = append(samples, vectorSample{Labels: r.Metric, Value: *v})
	}
	return samples, nil
}

// queryRange runs a range query and returns the resulting time series
// (single series only — callers pass a promql that resolves to one series).
func (p *promClient) queryRange(ctx context.Context, promql string, start, end time.Time, step string) ([]TimeSeriesPoint, error) {
	params := url.Values{}
	params.Set("query", promql)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", step)
	body, err := p.doGet(ctx, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode prometheus range response: %w", err)
	}
	if payload.Status != promStatusSuccess {
		return nil, fmt.Errorf("prometheus range query failed: %s", payload.Error)
	}
	if len(payload.Data.Result) == 0 {
		return nil, nil
	}
	points := make([]TimeSeriesPoint, 0, len(payload.Data.Result[0].Values))
	for _, pair := range payload.Data.Result[0].Values {
		ts, ok := pair[0].(float64)
		if !ok {
			continue
		}
		v, err := parseScalarValue(pair[1])
		if err != nil || v == nil {
			continue
		}
		points = append(points, TimeSeriesPoint{Time: time.Unix(int64(ts), 0), Value: *v})
	}
	return points, nil
}

func parseScalarValue(raw any) (Scalar, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", raw)
	}
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// NaN/Inf are valid Prometheus scalar encodings; treat as n/a rather
		// than a hard error.
		return nil, nil
	}
	return &parsed, nil
}

// TimeSeriesPoint is one sample of a range-query time series.
type TimeSeriesPoint struct {
	Time  time.Time
	Value float64
}

// ── Cluster selector convention ────────────────────────────────────────────

// clusterSelector returns entry's PromQL label matcher, following the same
// convention as the dashboard's buildClusterMatcher(): the cluster's
// configured Selector if set, else a fallback `cluster="<ID>"`.
func clusterSelector(entry cluster.ClusterEntry) string {
	if sel := strings.TrimSpace(entry.Selector); sel != "" {
		return sel
	}
	return fmt.Sprintf(`cluster="%s"`, entry.ID)
}

var clusterLabelRe = regexp.MustCompile(`\bcluster\s*=\s*"([^"]*)"`)

var regexMetaRe = regexp.MustCompile(`[\\^$.|?*+()\[\]{}]`)

// clusterLabelValue is the `cluster` label value a cluster's series carry.
//
// A cluster ID is this platform's own handle for a config entry; the label
// value is whatever the scrape pipeline stamps on the metric, and the two
// differ in practice (`bar` vs `prod-bar`). Report queries match on the
// cluster label alone, so they must use this — matching on the ID silently
// returns no series at all. Read out of the configured selector when it pins
// the label; otherwise the ID is the best guess available.
func clusterLabelValue(entry cluster.ClusterEntry) string {
	if m := clusterLabelRe.FindStringSubmatch(entry.Selector); m != nil {
		return m[1]
	}
	return entry.ID
}

// escapeRegexMeta keeps a label value a literal inside a `=~` alternation.
func escapeRegexMeta(s string) string {
	return regexMetaRe.ReplaceAllString(s, `\$0`)
}

// reportCluster pairs a cluster's display ID with the label value its metrics
// carry. The report shows the former and queries the latter.
type reportCluster struct {
	ID         string
	LabelValue string
}

// ── Daily report query engine (ported from dashboard/scripts/prometheus-report-core.ts) ──
//
// The legacy script (and this port) queries the bare `cluster` label with a
// regex alternation over cluster IDs, rather than each cluster's own
// Selector — preserving exact fidelity with the daily report that has run in
// production for months. The top-users and idle-detection queries below are
// not part of that legacy set and use the Selector-based convention instead,
// matching the dashboard's create-distribution route.

// QuerySpec is one named PromQL query making up a report.
type QuerySpec struct {
	Key         string
	Description string
	PromQL      string
}

func buildClusterSelectorRegex(clusterMatcher string) string {
	return fmt.Sprintf(`cluster=~"%s"`, clusterMatcher)
}

func buildEnvoySelector(clusterMatcher string) string {
	return fmt.Sprintf(`namespace="%s",cluster=~"%s",envoy_cluster_name="%s"`,
		agentboxNamespace, clusterMatcher, envoyClusterName)
}

// buildQueries returns the full set of daily-report queries for one scope
// (combined or single-cluster), ported 1:1 from buildQueries() in
// prometheus-report-core.ts.
func buildQueries(clusterMatcher, windowLiteral string) []QuerySpec {
	sel := buildClusterSelectorRegex(clusterMatcher)
	envoySel := buildEnvoySelector(clusterMatcher)

	var specs []QuerySpec

	gaugeSummary := func(metric, suffix, description string) {
		specs = append(specs,
			QuerySpec{
				Key:         suffix + "Current",
				Description: description + " current",
				PromQL:      fmt.Sprintf(`sum(%s{%s})`, metric, sel),
			},
			QuerySpec{
				Key:         suffix + "Avg",
				Description: description + " average over window",
				PromQL:      fmt.Sprintf(`avg_over_time((sum(%s{%s}))[%s:%s])`, metric, sel, windowLiteral, defaultSubqueryStep),
			},
			QuerySpec{
				Key:         suffix + "Peak",
				Description: description + " peak over window",
				PromQL:      fmt.Sprintf(`max_over_time((sum(%s{%s}))[%s:%s])`, metric, sel, windowLiteral, defaultSubqueryStep),
			},
		)
	}
	gaugeSummary("agentbox_sandboxpool_replicas_desired", "desiredReplicas", "Desired replicas")
	gaugeSummary("agentbox_sandboxpool_replicas_idle", "idleReplicas", "Idle replicas")
	gaugeSummary("agentbox_sandboxpool_replicas_running", "runningReplicas", "Running replicas")
	gaugeSummary("agentbox_sandboxpool_replicas_starting", "startingReplicas", "Starting replicas")
	gaugeSummary("agentbox_sandboxpool_replicas_stopping", "stoppingReplicas", "Stopping replicas")
	gaugeSummary("agentbox_sandboxpool_replicas_failed", "failedReplicas", "Failed replicas")

	histogramQuantile := func(key, description, metric string, quantile float64, extraSel string) {
		s := sel
		if extraSel != "" {
			s = sel + "," + extraSel
		}
		specs = append(specs, QuerySpec{
			Key:         key,
			Description: description,
			PromQL: fmt.Sprintf(`histogram_quantile(%s, sum by (le) (increase(%s_bucket{%s}[%s])))`,
				formatQuantile(quantile), metric, s, windowLiteral),
		})
	}
	histogramQuantile("claimP50", "Claim duration P50 (seconds)", "agentbox_sandbox_claim_duration_seconds", 0.50, `outcome="success"`)
	histogramQuantile("claimP90", "Claim duration P90 (seconds)", "agentbox_sandbox_claim_duration_seconds", 0.90, `outcome="success"`)
	histogramQuantile("claimP95", "Claim duration P95 (seconds)", "agentbox_sandbox_claim_duration_seconds", 0.95, `outcome="success"`)
	histogramQuantile("claimP99", "Claim duration P99 (seconds)", "agentbox_sandbox_claim_duration_seconds", 0.99, `outcome="success"`)
	histogramQuantile("recycleP50", "Recycle duration P50 (seconds)", "agentbox_sandbox_recycle_duration_seconds", 0.50, "")
	histogramQuantile("recycleP95", "Recycle duration P95 (seconds)", "agentbox_sandbox_recycle_duration_seconds", 0.95, "")
	histogramQuantile("recycleP99", "Recycle duration P99 (seconds)", "agentbox_sandbox_recycle_duration_seconds", 0.99, "")
	histogramQuantile("runningDurationP50", "Running duration P50 (seconds)", "agentbox_sandbox_running_duration_seconds", 0.50, "")
	histogramQuantile("runningDurationP95", "Running duration P95 (seconds)", "agentbox_sandbox_running_duration_seconds", 0.95, "")
	histogramQuantile("runningDurationP99", "Running duration P99 (seconds)", "agentbox_sandbox_running_duration_seconds", 0.99, "")

	envoyHistogramQuantile := func(key, description string, quantile float64) {
		specs = append(specs, QuerySpec{
			Key:         key,
			Description: description,
			PromQL: fmt.Sprintf(`histogram_quantile(%s, sum by (le) (increase(envoy_cluster_external_upstream_rq_time_bucket{%s}[%s])))`,
				formatQuantile(quantile), envoySel, windowLiteral),
		})
	}
	envoyHistogramQuantile("envoyP95", "Envoy external upstream request time P95", 0.95)
	envoyHistogramQuantile("envoyP99", "Envoy external upstream request time P99", 0.99)

	simple := func(key, description, promql string) {
		specs = append(specs, QuerySpec{Key: key, Description: description, PromQL: promql})
	}

	simple("createSuccess", "Successful sandbox creates", fmt.Sprintf(`sum(increase(agentbox_sandbox_create_total{%s,result="success"}[%s]))`, sel, windowLiteral))
	simple("createNoIdle", "Sandbox creates blocked by no idle replicas", fmt.Sprintf(`sum(increase(agentbox_sandbox_create_total{%s,result="no_idle"}[%s]))`, sel, windowLiteral))
	simple("createError", "Sandbox creates failed with error", fmt.Sprintf(`sum(increase(agentbox_sandbox_create_total{%s,result="error"}[%s]))`, sel, windowLiteral))

	simple("deleteCompleted", "Completed sandbox deletes", fmt.Sprintf(`sum(increase(agentbox_sandbox_delete_total{%s,stop_reason="Completed"}[%s]))`, sel, windowLiteral))
	simple("deleteCanceled", "Canceled sandbox deletes", fmt.Sprintf(`sum(increase(agentbox_sandbox_delete_total{%s,stop_reason="Canceled"}[%s]))`, sel, windowLiteral))
	simple("deleteReleased", "Released sandbox deletes", fmt.Sprintf(`sum(increase(agentbox_sandbox_delete_total{%s,stop_reason="Released"}[%s]))`, sel, windowLiteral))
	simple("deleteFailed", "Failed sandbox deletes", fmt.Sprintf(`sum(increase(agentbox_sandbox_delete_total{%s,stop_reason="Failed"}[%s]))`, sel, windowLiteral))
	simple("deleteTotal", "All sandbox deletes", fmt.Sprintf(`sum(increase(agentbox_sandbox_delete_total{%s}[%s]))`, sel, windowLiteral))

	simple("claimSuccess", "Sandbox claims: success", fmt.Sprintf(`sum(increase(agentbox_sandbox_claim_duration_seconds_count{%s,outcome="success"}[%s]))`, sel, windowLiteral))
	simple("claimNoIdle", "Sandbox claims: no idle replica available", fmt.Sprintf(`sum(increase(agentbox_sandbox_claim_duration_seconds_count{%s,outcome="no_idle"}[%s]))`, sel, windowLiteral))
	simple("claimTimeout", "Sandbox claims: timeout", fmt.Sprintf(`sum(increase(agentbox_sandbox_claim_duration_seconds_count{%s,outcome="timeout"}[%s]))`, sel, windowLiteral))
	simple("claimError", "Sandbox claims: error", fmt.Sprintf(`sum(increase(agentbox_sandbox_claim_duration_seconds_count{%s,outcome="error"}[%s]))`, sel, windowLiteral))

	simple("peakCreateSuccessRps", "Peak sandbox create success rate", fmt.Sprintf(`max_over_time((sum(rate(agentbox_sandbox_create_total{%s,result="success"}[5m])))[%s:5m])`, sel, windowLiteral))
	simple("peakCreateAttemptRps", "Peak sandbox create attempt rate", fmt.Sprintf(`max_over_time((sum(rate(agentbox_sandbox_create_total{%s}[5m])))[%s:5m])`, sel, windowLiteral))
	simple("peakHttpNativeRps", "Peak native HTTP API rate", fmt.Sprintf(`max_over_time((sum(rate(agentbox_http_requests_total{%s,api="native"}[5m])))[%s:5m])`, sel, windowLiteral))
	simple("peakHttpE2bRps", "Peak E2B HTTP API rate", fmt.Sprintf(`max_over_time((sum(rate(agentbox_http_requests_total{%s,api="e2b"}[5m])))[%s:5m])`, sel, windowLiteral))

	simple("httpNativeTotal", "Native HTTP API request total", fmt.Sprintf(`sum(increase(agentbox_http_requests_total{%s,api="native"}[%s]))`, sel, windowLiteral))
	simple("httpE2bTotal", "E2B HTTP API request total", fmt.Sprintf(`sum(increase(agentbox_http_requests_total{%s,api="e2b"}[%s]))`, sel, windowLiteral))
	simple("httpNativeP95", "Native HTTP API request duration P95", fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (increase(agentbox_http_request_duration_seconds_bucket{%s,api="native"}[%s])))`, sel, windowLiteral))
	simple("httpE2bP95", "E2B HTTP API request duration P95", fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (increase(agentbox_http_request_duration_seconds_bucket{%s,api="e2b"}[%s])))`, sel, windowLiteral))

	simple("envoyUpstreamTotal", "Envoy upstream request total", fmt.Sprintf(`sum(increase(envoy_cluster_external_upstream_rq{%s}[%s]))`, envoySel, windowLiteral))
	simple("envoy2xx", "Envoy upstream 2xx total", fmt.Sprintf(`sum(increase(envoy_cluster_external_upstream_rq_xx{%s,envoy_response_code_class="2"}[%s]))`, envoySel, windowLiteral))
	simple("envoy4xx", "Envoy upstream 4xx total", fmt.Sprintf(`sum(increase(envoy_cluster_external_upstream_rq_xx{%s,envoy_response_code_class="4"}[%s]))`, envoySel, windowLiteral))
	simple("envoy5xx", "Envoy upstream 5xx total", fmt.Sprintf(`sum(increase(envoy_cluster_external_upstream_rq_xx{%s,envoy_response_code_class="5"}[%s]))`, envoySel, windowLiteral))
	simple("peakEnvoyRps", "Peak Envoy upstream request rate", fmt.Sprintf(`max_over_time((sum(rate(envoy_cluster_external_upstream_rq{%s}[5m])))[%s:5m])`, envoySel, windowLiteral))

	return specs
}

func formatQuantile(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

// deriveMetrics computes ratios/sums that are not directly queryable,
// ported 1:1 from deriveMetrics() in prometheus-report-core.ts.
func deriveMetrics(m map[string]Scalar) map[string]Scalar {
	d := map[string]Scalar{}

	createAttemptTotal := sumScalars(m["createSuccess"], m["createNoIdle"], m["createError"])
	d["createAttemptTotal"] = createAttemptTotal
	d["createSuccessRate"] = divide(m["createSuccess"], createAttemptTotal)
	d["createNoIdleRate"] = divide(m["createNoIdle"], createAttemptTotal)
	d["createErrorRate"] = divide(m["createError"], createAttemptTotal)

	d["deleteFailedRate"] = divide(m["deleteFailed"], m["deleteTotal"])
	d["deleteReleasedRate"] = divide(m["deleteReleased"], m["deleteTotal"])

	claimTotal := sumScalars(m["claimSuccess"], m["claimNoIdle"], m["claimTimeout"], m["claimError"])
	d["claimTotal"] = claimTotal
	d["claimSuccessRate"] = divide(m["claimSuccess"], claimTotal)
	d["claimTimeoutRate"] = divide(m["claimTimeout"], claimTotal)
	d["claimErrorRate"] = divide(m["claimError"], claimTotal)

	perMinute := func(rps Scalar) Scalar {
		if rps == nil {
			return nil
		}
		v := *rps * 60
		return &v
	}
	d["peakCreateSuccessPerMinute"] = perMinute(m["peakCreateSuccessRps"])
	d["peakCreateAttemptPerMinute"] = perMinute(m["peakCreateAttemptRps"])
	d["peakEnvoyPerMinute"] = perMinute(m["peakEnvoyRps"])
	d["peakHttpNativePerMinute"] = perMinute(m["peakHttpNativeRps"])
	d["peakHttpE2bPerMinute"] = perMinute(m["peakHttpE2bRps"])

	d["envoy2xxRate"] = divide(m["envoy2xx"], m["envoyUpstreamTotal"])
	d["envoy5xxRate"] = divide(m["envoy5xx"], m["envoyUpstreamTotal"])

	return d
}

// ScopeCombined is the Scope value of the cross-cluster rollup entry, as
// opposed to the per-cluster entries which carry the cluster ID.
const ScopeCombined = "combined"

// ScopeReport is one report scope: either the combined view across all
// clusters, or a single cluster.
type ScopeReport struct {
	Scope          string
	ClusterMatcher string
	Metrics        map[string]Scalar
	Derived        map[string]Scalar
	// HasData is false when every query in this scope returned no series at
	// all — the signature of a cluster with no SandboxPool deployed yet
	// rather than a cluster that is merely quiet. The daily report card must
	// call this out explicitly instead of rendering as if nothing happened.
	HasData bool
}

// collectScope runs every query for one scope concurrently and derives the
// composite metrics.
// A failed query leaves its metric nil rather than aborting the scope: a
// partially-answered report is still worth sending, and HasData distinguishes
// "everything came back empty" from "some values are missing".
func collectScope(ctx context.Context, pc *promClient, scope, clusterMatcher string, at time.Time, windowLiteral string) ScopeReport {
	specs := buildQueries(clusterMatcher, windowLiteral)
	values := make([]Scalar, len(specs))

	var wg sync.WaitGroup
	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec QuerySpec) {
			defer wg.Done()
			v, err := pc.query(ctx, spec.PromQL, at)
			if err != nil {
				values[i] = nil
				return
			}
			values[i] = v
		}(i, spec)
	}
	wg.Wait()

	metrics := make(map[string]Scalar, len(specs))
	hasData := false
	for i, spec := range specs {
		metrics[spec.Key] = values[i]
		if values[i] != nil {
			hasData = true
		}
	}

	return ScopeReport{
		Scope:          scope,
		ClusterMatcher: clusterMatcher,
		Metrics:        metrics,
		Derived:        deriveMetrics(metrics),
		HasData:        hasData,
	}
}

// ReportWindow describes the absolute time window a Report covers.
type ReportWindow struct {
	Start   time.Time
	End     time.Time
	Seconds int64
	Literal string
}

// Report is the full daily-report payload: a combined scope across every
// cluster plus one scope per individual cluster.
type Report struct {
	GeneratedAt   time.Time
	PrometheusURL string
	Window        ReportWindow
	Clusters      []string
	Scopes        []ScopeReport
	// NoDataClusters lists cluster IDs whose per-cluster scope returned no
	// data at all for this window (e.g. a newly-added cluster with no
	// SandboxPool deployed yet). The card renders normally for every other
	// cluster and calls these out in a footer line instead of silently
	// omitting them.
	NoDataClusters []string
}

// buildReport assembles the combined + per-cluster scopes for windowLiteral
// ending at end, ported from buildReport() in prometheus-report-core.ts.
// Per-cluster scopes are collected sequentially (matching the original
// script) since they share the same Prometheus backend and running them
// concurrently would multiply the query load with no latency benefit for a
// once-a-day report.
func buildReport(ctx context.Context, pc *promClient, clusters []reportCluster, windowLiteral string, end time.Time) (*Report, error) {
	windowSeconds, err := parseWindowLiteral(windowLiteral)
	if err != nil {
		return nil, err
	}
	start := end.Add(-time.Duration(windowSeconds) * time.Second)

	report := &Report{
		GeneratedAt:   time.Now().UTC(),
		PrometheusURL: pc.baseURL,
		Window: ReportWindow{
			Start:   start,
			End:     end,
			Seconds: windowSeconds,
			Literal: windowLiteral,
		},
		Clusters: clusterIDs(clusters),
	}

	report.Scopes = append(report.Scopes,
		collectScope(ctx, pc, ScopeCombined, combinedMatcher(clusters), end, windowLiteral))

	for _, c := range clusters {
		// Scope carries the ID for display; the matcher must be the label value.
		scope := collectScope(ctx, pc, c.ID, escapeRegexMeta(c.LabelValue), end, windowLiteral)
		report.Scopes = append(report.Scopes, scope)
		if !scope.HasData {
			report.NoDataClusters = append(report.NoDataClusters, c.ID)
		}
	}

	return report, nil
}

// reportClusters returns every cluster configured in clusters.yaml with the
// label value its metrics carry. The daily report covers all of them
// unconditionally — it is a broadcast operational notification with no
// per-viewer audience, so the ACL-style Visible field on ClusterEntry (which
// restricts which dashboard users may see a cluster) does not apply here.
func (s *Service) reportClusters() []reportCluster {
	entries := s.clusters.All()
	out := make([]reportCluster, len(entries))
	for i, e := range entries {
		out[i] = reportCluster{ID: e.ID, LabelValue: clusterLabelValue(e)}
	}
	return out
}

// clusterIDs projects the display IDs, for the report's cluster list.
func clusterIDs(clusters []reportCluster) []string {
	ids := make([]string, len(clusters))
	for i, c := range clusters {
		ids[i] = c.ID
	}
	return ids
}

// combinedMatcher is the regex body matching every cluster in the set.
func combinedMatcher(clusters []reportCluster) string {
	parts := make([]string, len(clusters))
	for i, c := range clusters {
		parts[i] = escapeRegexMeta(c.LabelValue)
	}
	return strings.Join(parts, "|")
}

func parseWindowLiteral(literal string) (int64, error) {
	if len(literal) < 2 {
		return 0, fmt.Errorf("invalid window literal %q", literal)
	}
	unit := literal[len(literal)-1]
	n, err := strconv.ParseInt(literal[:len(literal)-1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid window literal %q: %w", literal, err)
	}
	var factor int64
	switch unit {
	case 's':
		factor = 1
	case 'm':
		factor = 60
	case 'h':
		factor = 3600
	case 'd':
		factor = 86400
	case 'w':
		factor = 604800
	default:
		return 0, fmt.Errorf("invalid window literal %q: unknown unit %q", literal, string(unit))
	}
	return n * factor, nil
}

// collectChartData fetches the desired/running replica trend for the
// replica chart on the daily report card.
type chartData struct {
	Desired []TimeSeriesPoint
	Running []TimeSeriesPoint
}

func collectChartData(ctx context.Context, pc *promClient, clusterMatcher string, start, end time.Time, step string) (*chartData, error) {
	sel := buildClusterSelectorRegex(clusterMatcher)

	var wg sync.WaitGroup
	var desired, running []TimeSeriesPoint
	var desiredErr, runningErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		desired, desiredErr = pc.queryRange(ctx, fmt.Sprintf(`sum(agentbox_sandboxpool_replicas_desired{%s})`, sel), start, end, step)
	}()
	go func() {
		defer wg.Done()
		running, runningErr = pc.queryRange(ctx, fmt.Sprintf(`sum(agentbox_sandboxpool_replicas_running{%s})`, sel), start, end, step)
	}()
	wg.Wait()

	if desiredErr != nil {
		return nil, desiredErr
	}
	if runningErr != nil {
		return nil, runningErr
	}
	return &chartData{Desired: desired, Running: running}, nil
}

// ── Top users (selector-based convention) ──────────────────────────────────

// UserCount is one row of the daily report's top-creators ranking.
type UserCount struct {
	User  string
	Count float64
}

// topUsers ranks users by sandbox-create count over windowLiteral, across
// every configured cluster — the daily report card's "Top 10 users" panel.
// Each cluster is queried with its own Selector (falling back to
// `cluster="<ID>"`), matching the dashboard's create-distribution convention,
// and results are merged locally rather than queried as one bare cluster
// regex: a cluster's Selector may encode more than its ID, so a regex over
// IDs alone would silently match the wrong series. A cluster whose query
// fails is excluded from the ranking (fail-open by exclusion) rather than
// aborting the whole report.
func (s *Service) topUsers(ctx context.Context, pc *promClient, windowLiteral string, limit int) []UserCount {
	entries := s.clusters.All()
	counts := map[string]float64{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, entry := range entries {
		wg.Add(1)
		go func(entry cluster.ClusterEntry) {
			defer wg.Done()
			sel := clusterSelector(entry)
			promql := fmt.Sprintf(`sum by (user) (increase(agentbox_sandbox_create_total{%s,result="success"}[%s]))`, sel, windowLiteral)
			rows, err := pc.queryVector(ctx, promql, time.Time{})
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, r := range rows {
				counts[r.Labels["user"]] += r.Value
			}
		}(entry)
	}
	wg.Wait()

	rows := make([]UserCount, 0, len(counts))
	for user, count := range counts {
		rows = append(rows, UserCount{User: user, Count: count})
	}
	sortUserCountsDesc(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func sortUserCountsDesc(rows []UserCount) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Count > rows[j-1].Count; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// ── Idle detection (selector-based convention) ─────────────────────────────

// ClusterIdleStatus is the per-cluster idle-check outcome. Ok is false when
// the query failed or the cluster is unknown — the result must be treated
// as indeterminate and never as "idle".
type ClusterIdleStatus struct {
	ClusterID string
	Idle      bool
	Ok        bool
}

// checkClusterIdle reports whether clusterID has had zero sandbox-create
// activity (any result) in the last thresholdMinutes. A query failure is
// reported as Ok=false, not as Idle=true — callers must skip the judgment
// cycle rather than risk a false idle-alert firing on a monitoring blip.
func (s *Service) checkClusterIdle(ctx context.Context, pc *promClient, clusterID string, thresholdMinutes int) ClusterIdleStatus {
	entry, ok := s.clusters.Get(clusterID)
	if !ok {
		return ClusterIdleStatus{ClusterID: clusterID, Ok: false}
	}
	sel := clusterSelector(entry)
	promql := fmt.Sprintf(`sum(increase(agentbox_sandbox_create_total{%s}[%dm]))`, sel, thresholdMinutes)
	v, err := pc.query(ctx, promql, time.Time{})
	if err != nil || v == nil {
		return ClusterIdleStatus{ClusterID: clusterID, Ok: false}
	}
	return ClusterIdleStatus{ClusterID: clusterID, Idle: *v == 0, Ok: true}
}
