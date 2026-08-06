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
	"testing"
	"time"
)

// The last-fired date has to be persisted, not held in a local variable: a
// restart past the send hour otherwise reads "today hasn't fired yet" and
// sends again, so a day of deploys becomes a day of duplicate cards.
func TestSeedDailyReportDateMarksTodayWhenStartedPastSendHour(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	cfg := defaultConfig()
	// Send hour 0 guarantees "now" is past the target whenever this runs.
	cfg.DailyReport.SendHourCST = 0
	if _, err := s.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	s.seedDailyReportDate(ctx)

	st, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	want := time.Now().In(shanghai()).Format("2006-01-02")
	if st.LastDailyReportDate != want {
		t.Errorf("LastDailyReportDate = %q, want %q — a restart past the send hour must not re-fire",
			st.LastDailyReportDate, want)
	}
}

// Starting before the send hour must leave the day open, or a morning deploy
// would suppress that day's report entirely.
func TestSeedDailyReportDateLeavesTodayOpenBeforeSendHour(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	cfg := defaultConfig()
	// Send hour 23 is still ahead unless this runs in the last hour of the day.
	if time.Now().In(shanghai()).Hour() >= 23 {
		t.Skip("run within the final hour of the day; no future send hour to assert against")
	}
	cfg.DailyReport.SendHourCST = 23
	if _, err := s.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	s.seedDailyReportDate(ctx)

	st, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.LastDailyReportDate != "" {
		t.Errorf("LastDailyReportDate = %q, want empty — today's report has not been sent yet",
			st.LastDailyReportDate)
	}
}

// Scheduler state lives in its own ConfigMap key because UpdateConfig
// replaces config.json wholesale; a shared key would lose the date on every
// admin edit and bring the duplicate cards back.
func TestUpdateConfigPreservesSchedulerState(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	if err := s.SaveState(ctx, runtimeState{LastDailyReportDate: "2026-08-06"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	cfg := defaultConfig()
	cfg.DailyReport.SendHourCST = 7
	if _, err := s.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	st, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.LastDailyReportDate != "2026-08-06" {
		t.Errorf("LastDailyReportDate = %q, want %q after a config edit",
			st.LastDailyReportDate, "2026-08-06")
	}
}
