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
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	dailyReportTickInterval = time.Minute
	idleAlertTickInterval   = time.Minute
	chartRangeStep          = "15m"
)

// Run starts the daily-report and idle-alert background loops. It blocks
// until ctx is canceled.
func (s *Service) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	s.ensureConfigMap(ctx)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.runDailyReportLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		s.runIdleAlertLoop(ctx)
	}()
	wg.Wait()
}

// runDailyReportLoop fires the daily report at most once per calendar day,
// the first tick at or after DailyReportConfig.SendHourCST in Asia/Shanghai.
//
// A naive `now.Minute() == 0` check would miss the target: ticker ticks are
// offset from process start time, not wall-clock aligned, so they rarely
// land exactly on :00. Comparing against the target time directly, guarded
// by lastFiredDate for at-most-once-per-day, is correct regardless of tick
// phase.
func (s *Service) runDailyReportLoop(ctx context.Context) {
	ticker := time.NewTicker(dailyReportTickInterval)
	defer ticker.Stop()

	lastFiredDate := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cfg, err := s.LoadConfig(ctx)
			if err != nil {
				log.Printf("wsproxy: notify: load config for daily report tick failed: %v", err)
				continue
			}
			if !cfg.DailyReport.Enabled {
				continue
			}

			now := time.Now().In(shanghai())
			todayKey := now.Format("2006-01-02")
			if lastFiredDate == todayKey {
				continue
			}
			target := time.Date(now.Year(), now.Month(), now.Day(), cfg.DailyReport.SendHourCST, 0, 0, 0, shanghai())
			if now.Before(target) {
				continue
			}
			lastFiredDate = todayKey
			s.sendDailyReport(ctx, target)
		}
	}
}

// SendDailyReportNow builds and sends a daily report on demand, for the
// admin "trigger now" API — the 24h window ends at now rather than waiting
// for the next scheduled tick.
func (s *Service) SendDailyReportNow(ctx context.Context, now time.Time) {
	s.sendDailyReport(ctx, now)
}

// sendDailyReport builds and sends one daily report for the 24h window
// ending at end (the day's aligned send time, not wall-clock "now", so
// window boundaries are stable and reproducible), recording the outcome in
// history regardless of success.
func (s *Service) sendDailyReport(ctx context.Context, end time.Time) {
	const windowLiteral = "1d"

	pc := newPromClient(s.prometheusURL, s.prometheusToken)
	clusterIDs := s.allClusterIDs()

	report, err := buildReport(ctx, pc, clusterIDs, windowLiteral, end)
	if err != nil {
		log.Printf("wsproxy: notify: build daily report failed: %v", err)
		s.appendHistory(ctx, HistoryEntry{Time: time.Now().UTC(), Type: HistoryTypeDailyReport, Result: ResultFailure, Detail: err.Error()})
		return
	}

	combinedMatcher := strings.Join(clusterIDs, "|")
	chart, err := collectChartData(ctx, pc, combinedMatcher, report.Window.Start, report.Window.End, chartRangeStep)
	if err != nil {
		log.Printf("wsproxy: notify: collect daily report chart data failed (rendering without chart): %v", err)
		chart = nil
	}

	top := s.topUsers(ctx, pc, windowLiteral, 10)
	card := buildFeishuCard(report, chart, top, end.Format("2006-01-02"))

	if err := sendToFeishu(ctx, s.feishuWebhook, card); err != nil {
		log.Printf("wsproxy: notify: send daily report to feishu failed: %v", err)
		s.appendHistory(ctx, HistoryEntry{Time: time.Now().UTC(), Type: HistoryTypeDailyReport, Result: ResultFailure, Detail: err.Error()})
		return
	}
	s.appendHistory(ctx, HistoryEntry{Time: time.Now().UTC(), Type: HistoryTypeDailyReport, Result: ResultSuccess})
}

// runIdleAlertLoop periodically evaluates the idle-cluster condition while
// the alert is armed, firing (and auto-disarming) the first time every
// watched cluster is simultaneously idle.
func (s *Service) runIdleAlertLoop(ctx context.Context) {
	ticker := time.NewTicker(idleAlertTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkIdleAlert(ctx)
		}
	}
}

func (s *Service) checkIdleAlert(ctx context.Context) {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		log.Printf("wsproxy: notify: load config for idle alert tick failed: %v", err)
		return
	}
	if !cfg.IdleAlert.Enabled || !cfg.IdleAlert.Armed || len(cfg.IdleAlert.WatchedClusters) == 0 {
		return
	}

	pc := newPromClient(s.prometheusURL, s.prometheusToken)
	watched := cfg.IdleAlert.WatchedClusters
	statuses := make([]ClusterIdleStatus, len(watched))

	var wg sync.WaitGroup
	for i, id := range watched {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			statuses[i] = s.checkClusterIdle(ctx, pc, id, cfg.IdleAlert.IdleThresholdMinutes)
		}(i, id)
	}
	wg.Wait()

	allIdle := true
	for _, st := range statuses {
		if !st.Ok {
			// Indeterminate: skip this judgment cycle entirely rather than
			// risk treating a query failure as "idle".
			return
		}
		if !st.Idle {
			allIdle = false
		}
	}
	if !allIdle {
		return
	}

	card := buildIdleAlertCard(watched, cfg.IdleAlert.IdleThresholdMinutes)
	sendErr := sendToFeishu(ctx, s.feishuWebhook, card)

	// Auto-disarm unconditionally: a transient Feishu failure must not leave
	// the alert armed to re-fire every tick once clusters go idle.
	if _, err := s.DisarmIdleAlert(ctx); err != nil {
		log.Printf("wsproxy: notify: disarm idle alert after fire failed: %v", err)
	}

	if sendErr != nil {
		log.Printf("wsproxy: notify: send idle alert to feishu failed: %v", sendErr)
		s.appendHistory(ctx, HistoryEntry{Time: time.Now().UTC(), Type: HistoryTypeIdleAlert, Result: ResultFailure, Detail: sendErr.Error()})
		return
	}
	s.appendHistory(ctx, HistoryEntry{Time: time.Now().UTC(), Type: HistoryTypeIdleAlert, Result: ResultSuccess})
}

// buildIdleAlertCard builds the (Go-only; no TS equivalent) idle-cluster
// alert card.
func buildIdleAlertCard(watchedClusters []string, thresholdMinutes int) FeishuCard {
	return FeishuCard{
		Schema: "2.0",
		Header: FeishuCardHead{
			Title:    FeishuCardElement{Tag: "plain_text", Content: "⚠️ AgentBox 空闲告警"},
			Template: "orange",
		},
		Body: FeishuCardBody{
			Elements: []FeishuCardElement{
				md(fmt.Sprintf("以下集群已连续 **%d 分钟** 无任何沙箱创建请求：\n%s",
					thresholdMinutes, strings.Join(watchedClusters, ", "))),
				md("_本告警已自动解除布防，需在 /admin 页面手动重新布防_"),
				md(fmt.Sprintf("_触发于 %s_", time.Now().In(shanghai()).Format("2006-01-02 15:04:05"))),
			},
		},
	}
}
