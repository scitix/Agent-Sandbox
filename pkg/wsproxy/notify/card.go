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
	"fmt"
	"strings"
	"time"
)

// ── Feishu card element builders ────────────────────────────────────────────
//
// Ported 1:1 from dashboard/scripts/feishu-daily-report.ts's card builders so
// the two implementations render visually identical cards. That script stays
// an untouched offline debugging tool; this is the independent production
// port that the Hub schedules and sends.

// FeishuCardElement is one element within a Feishu interactive card body, or
// a column within a column_set. The tag determines which fields are used.
type FeishuCardElement struct {
	Tag string `json:"tag"`

	// markdown
	Content   string `json:"content,omitempty"`
	TextAlign string `json:"text_align,omitempty"`

	// column_set
	HorizontalSpacing string             `json:"horizontal_spacing,omitempty"`
	FlexMode          string             `json:"flex_mode,omitempty"`
	Columns           []FeishuCardColumn `json:"columns,omitempty"`

	// collapsible_panel
	Expanded        bool                   `json:"expanded,omitempty"`
	Header          *FeishuCollapsibleHead `json:"header,omitempty"`
	Border          map[string]string      `json:"border,omitempty"`
	VerticalSpacing string                 `json:"vertical_spacing,omitempty"`
	Padding         string                 `json:"padding,omitempty"`
	Elements        []FeishuCardElement    `json:"elements,omitempty"`

	// chart (VChart)
	ChartSpec map[string]any `json:"chart_spec,omitempty"`
}

// FeishuCardColumn is one column within a column_set element.
type FeishuCardColumn struct {
	Tag             string              `json:"tag"`
	Width           string              `json:"width,omitempty"`
	Weight          int                 `json:"weight,omitempty"`
	VerticalAlign   string              `json:"vertical_align,omitempty"`
	BackgroundStyle string              `json:"background_style,omitempty"`
	Padding         string              `json:"padding,omitempty"`
	Elements        []FeishuCardElement `json:"elements,omitempty"`
}

// FeishuCollapsibleHead is the header of a collapsible_panel element.
type FeishuCollapsibleHead struct {
	Title FeishuCardElement `json:"title"`
}

// FeishuCard is a Feishu (Lark) interactive card, schema 2.0.
type FeishuCard struct {
	Schema string         `json:"schema"`
	Header FeishuCardHead `json:"header"`
	Body   FeishuCardBody `json:"body"`
}

// FeishuCardHead is the card header: title + accent color.
type FeishuCardHead struct {
	Title    FeishuCardElement `json:"title"`
	Template string            `json:"template"`
}

// FeishuCardBody holds the card's element list.
type FeishuCardBody struct {
	Elements []FeishuCardElement `json:"elements"`
}

func md(content string) FeishuCardElement {
	return FeishuCardElement{Tag: "markdown", Content: content, TextAlign: "left"}
}

func columnSet(columns ...FeishuCardColumn) FeishuCardElement {
	return FeishuCardElement{
		Tag:               "column_set",
		HorizontalSpacing: "default",
		FlexMode:          "bisect",
		Columns:           columns,
	}
}

// collapsiblePanel renders a section the reader opens on demand. Panels are
// always shipped collapsed: the card is read on a phone and the summary above
// it is the part that has to fit on screen.
func collapsiblePanel(title string, elements ...FeishuCardElement) FeishuCardElement {
	return FeishuCardElement{
		Tag:      "collapsible_panel",
		Expanded: false,
		Header: &FeishuCollapsibleHead{
			Title: FeishuCardElement{Tag: "markdown", Content: title},
		},
		Border:          map[string]string{"color": "grey"},
		VerticalSpacing: "8px",
		Padding:         "4px 8px 4px 8px",
		Elements:        elements,
	}
}

func statCard(label, value, subtext string) FeishuCardColumn {
	return FeishuCardColumn{
		Tag:             "column",
		Width:           "weighted",
		Weight:          1,
		VerticalAlign:   "top",
		BackgroundStyle: "grey",
		Padding:         "12px",
		Elements: []FeishuCardElement{
			md(fmt.Sprintf("%s\n**%s**\n%s", label, value, subtext)),
		},
	}
}

// ── Number formatting (ported from prometheus-report-core.ts's formatters) ─

// naLabel is what a nil Scalar renders as: the query returned no series, so
// there is no value to show rather than a zero.
const naLabel = "N/A"

func formatNumber(v Scalar) string {
	if v == nil {
		return naLabel
	}
	if *v == float64(int64(*v)) {
		return fmt.Sprintf("%d", int64(*v))
	}
	return fmt.Sprintf("%.1f", *v)
}

func formatPercent(v Scalar) string {
	if v == nil {
		return naLabel
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

func formatSeconds(v Scalar) string {
	if v == nil {
		return naLabel
	}
	if *v < 1 {
		return fmt.Sprintf("%dms", int64(*v*1000))
	}
	if *v < 60 {
		return fmt.Sprintf("%.1fs", *v)
	}
	return fmt.Sprintf("%.1fmin", *v/60)
}

// ── Replica trend chart (VChart line chart) ─────────────────────────────────

func buildReplicaChart(desired, running []TimeSeriesPoint) FeishuCardElement {
	formatTime := func(t time.Time) string {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			loc = time.UTC
		}
		return t.In(loc).Format("01/02 15:04")
	}

	type point struct {
		Time   string  `json:"time"`
		Value  float64 `json:"value"`
		Series string  `json:"series"`
	}
	values := make([]point, 0, len(desired)+len(running))
	for _, p := range desired {
		values = append(values, point{Time: formatTime(p.Time), Value: p.Value, Series: "总量"})
	}
	for _, p := range running {
		values = append(values, point{Time: formatTime(p.Time), Value: p.Value, Series: "运行中"})
	}

	spec := map[string]any{
		"type": "line",
		"title": map[string]any{
			"visible": true,
			"text":    "预热池副本趋势",
		},
		"data": []map[string]any{
			{"id": "replicaData", "values": values},
		},
		"xField":      "time",
		"yField":      "value",
		"seriesField": "series",
		"legends": map[string]any{
			"visible":  true,
			"position": "middle",
			"orient":   "bottom",
		},
	}

	return FeishuCardElement{Tag: "chart", ChartSpec: spec}
}

// ── Scope detail (per-scope markdown grid) ──────────────────────────────────

func buildScopeDetail(scope ScopeReport) []FeishuCardElement {
	m := scope.Metrics
	d := scope.Derived

	poolStatus := md(fmt.Sprintf(
		"**预热池状态**\n期望副本: %s (avg %s / peak %s)\n空闲: %s (avg %s)\n运行中: %s (avg %s)\n启动中: %s / 停止中: %s / 失败: %s",
		formatNumber(m["desiredReplicasCurrent"]), formatNumber(m["desiredReplicasAvg"]), formatNumber(m["desiredReplicasPeak"]),
		formatNumber(m["idleReplicasCurrent"]), formatNumber(m["idleReplicasAvg"]),
		formatNumber(m["runningReplicasCurrent"]), formatNumber(m["runningReplicasAvg"]),
		formatNumber(m["startingReplicasCurrent"]), formatNumber(m["stoppingReplicasCurrent"]), formatNumber(m["failedReplicasCurrent"]),
	))

	lifecycle := md(fmt.Sprintf(
		"**生命周期**\n创建: 成功 %s / 无空闲 %s / 错误 %s（成功率 %s）\n删除: 完成 %s / 取消 %s / 释放 %s / 失败 %s",
		formatNumber(m["createSuccess"]), formatNumber(m["createNoIdle"]), formatNumber(m["createError"]), formatPercent(d["createSuccessRate"]),
		formatNumber(m["deleteCompleted"]), formatNumber(m["deleteCanceled"]), formatNumber(m["deleteReleased"]), formatNumber(m["deleteFailed"]),
	))

	perf := md(fmt.Sprintf(
		"**性能**\n领取: P50 %s / P90 %s / P95 %s / P99 %s\n回收: P50 %s / P95 %s / P99 %s\n运行时长: P50 %s / P95 %s / P99 %s",
		formatSeconds(m["claimP50"]), formatSeconds(m["claimP90"]), formatSeconds(m["claimP95"]), formatSeconds(m["claimP99"]),
		formatSeconds(m["recycleP50"]), formatSeconds(m["recycleP95"]), formatSeconds(m["recycleP99"]),
		formatSeconds(m["runningDurationP50"]), formatSeconds(m["runningDurationP95"]), formatSeconds(m["runningDurationP99"]),
	))

	api := md(fmt.Sprintf(
		"**API**\nNative: %s 次, P95 %s\nE2B: %s 次, P95 %s\n峰值创建速率: 成功 %s/min, 尝试 %s/min",
		formatNumber(m["httpNativeTotal"]), formatSeconds(m["httpNativeP95"]),
		formatNumber(m["httpE2bTotal"]), formatSeconds(m["httpE2bP95"]),
		formatNumber(d["peakCreateSuccessPerMinute"]), formatNumber(d["peakCreateAttemptPerMinute"]),
	))

	envoy := md(fmt.Sprintf(
		"**Envoy**\n请求总数: %s（2xx %s / 5xx %s）\nP95 %s / P99 %s / 峰值速率 %s/min",
		formatNumber(m["envoyUpstreamTotal"]), formatPercent(d["envoy2xxRate"]), formatPercent(d["envoy5xxRate"]),
		formatSeconds(m["envoyP95"]), formatSeconds(m["envoyP99"]), formatNumber(d["peakEnvoyPerMinute"]),
	))

	return []FeishuCardElement{
		columnSet(
			FeishuCardColumn{Tag: "column", Width: "weighted", Weight: 1, VerticalAlign: "top", Elements: []FeishuCardElement{poolStatus}},
			FeishuCardColumn{Tag: "column", Width: "weighted", Weight: 1, VerticalAlign: "top", Elements: []FeishuCardElement{lifecycle}},
		),
		columnSet(
			FeishuCardColumn{Tag: "column", Width: "weighted", Weight: 1, VerticalAlign: "top", Elements: []FeishuCardElement{perf}},
			FeishuCardColumn{Tag: "column", Width: "weighted", Weight: 1, VerticalAlign: "top", Elements: []FeishuCardElement{api}},
		),
		envoy,
	}
}

// ── Header color / severity ─────────────────────────────────────────────────

// determineHeaderColor picks the card's accent color from the combined
// scope's derived error rates, ported 1:1 from determineHeaderColor() in
// feishu-daily-report.ts.
func determineHeaderColor(report *Report) string {
	var combined *ScopeReport
	for i := range report.Scopes {
		if report.Scopes[i].Scope == ScopeCombined {
			combined = &report.Scopes[i]
			break
		}
	}
	if combined == nil {
		return "blue"
	}
	d := combined.Derived
	gt := func(v Scalar, threshold float64) bool { return v != nil && *v > threshold }

	if gt(d["createErrorRate"], 0.05) || gt(d["envoy5xxRate"], 0.01) || gt(combined.Metrics["failedReplicasCurrent"], 10) {
		return "red"
	}
	if gt(d["createNoIdleRate"], 0.05) || gt(d["claimTimeoutRate"], 0.01) {
		return "orange"
	}
	return "green"
}

// ── Top full card assembly ──────────────────────────────────────────────────

// buildFeishuCard assembles the full daily-report card, ported from
// buildFeishuCard() in feishu-daily-report.ts, plus a
// Top-10-users panel and a "no data" footer for clusters with no data at all
// this period.
func buildFeishuCard(report *Report, chart *chartData, topUsers []UserCount, dateLabel string) FeishuCard {
	var combined *ScopeReport
	for i := range report.Scopes {
		if report.Scopes[i].Scope == ScopeCombined {
			combined = &report.Scopes[i]
			break
		}
	}

	elements := []FeishuCardElement{
		md(fmt.Sprintf("**统计区间**: %s ~ %s",
			report.Window.Start.In(shanghai()).Format("2006-01-02 15:04"),
			report.Window.End.In(shanghai()).Format("2006-01-02 15:04"))),
	}

	if combined != nil {
		m, d := combined.Metrics, combined.Derived
		elements = append(elements,
			columnSet(
				statCard("🚀 创建数", formatNumber(d["createAttemptTotal"]), fmt.Sprintf("成功率 %s", formatPercent(d["createSuccessRate"]))),
				statCard("✅ 成功率", formatPercent(d["createSuccessRate"]), fmt.Sprintf("成功 %s 次", formatNumber(m["createSuccess"]))),
			),
			columnSet(
				statCard("⏱️ 领取耗时P95", formatSeconds(m["claimP95"]), fmt.Sprintf("P99 %s", formatSeconds(m["claimP99"]))),
				statCard("🌐 Envoy请求数", formatNumber(m["envoyUpstreamTotal"]), fmt.Sprintf("5xx率 %s", formatPercent(d["envoy5xxRate"]))),
			),
		)
	}

	if chart != nil && (len(chart.Desired) > 0 || len(chart.Running) > 0) {
		elements = append(elements, buildReplicaChart(chart.Desired, chart.Running))
	}

	if combined != nil {
		elements = append(elements, collapsiblePanel("📊 汇总详细指标", buildScopeDetail(*combined)...))
	}

	for i := range report.Scopes {
		scope := report.Scopes[i]
		if scope.Scope == ScopeCombined {
			continue
		}
		title := fmt.Sprintf("📍 集群: %s", scope.Scope)
		if !scope.HasData {
			elements = append(elements, collapsiblePanel(title, md("_本周期无数据_")))
			continue
		}
		elements = append(elements, collapsiblePanel(title, buildScopeDetail(scope)...))
	}

	if len(topUsers) > 0 {
		lines := make([]string, 0, len(topUsers))
		for i, u := range topUsers {
			lines = append(lines, fmt.Sprintf("%d. %s — %s 次", i+1, u.User, formatNumber(f64(u.Count))))
		}
		elements = append(elements, collapsiblePanel("👤 当日创建量 Top 10 用户", md(strings.Join(lines, "\n"))))
	}

	if len(report.NoDataClusters) > 0 {
		elements = append(elements, md(fmt.Sprintf("⚠️ 以下集群本周期无数据: %s", strings.Join(report.NoDataClusters, ", "))))
	}

	elements = append(elements, md(fmt.Sprintf("_自动生成于 %s_", report.GeneratedAt.In(shanghai()).Format("2006-01-02 15:04:05"))))

	return FeishuCard{
		Schema: "2.0",
		Header: FeishuCardHead{
			Title:    FeishuCardElement{Tag: "plain_text", Content: fmt.Sprintf("📊 AgentBox 每日报告 - %s", dateLabel)},
			Template: determineHeaderColor(report),
		},
		Body: FeishuCardBody{Elements: elements},
	}
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.UTC
	}
	return loc
}
