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

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestService() *Service {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	return New(Params{
		Client:        k8sClient,
		Namespace:     "agentbox-system",
		ConfigMapName: "agentbox-notifications",
		PrometheusURL: "http://prometheus.example.com",
	})
}

func TestLoadConfigDefaultsWhenConfigMapMissing(t *testing.T) {
	s := newTestService()
	cfg, err := s.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := defaultConfig()
	if cfg.DailyReport != want.DailyReport {
		t.Errorf("DailyReport = %+v, want %+v", cfg.DailyReport, want.DailyReport)
	}
	if cfg.IdleAlert.Enabled != want.IdleAlert.Enabled || cfg.IdleAlert.IdleThresholdMinutes != want.IdleAlert.IdleThresholdMinutes {
		t.Errorf("IdleAlert = %+v, want %+v", cfg.IdleAlert, want.IdleAlert)
	}
}

func TestUpdateConfigRoundTrips(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	in := Config{
		DailyReport: DailyReportConfig{Enabled: false, SendHourCST: 14},
		IdleAlert: IdleAlertConfig{
			Enabled:              true,
			WatchedClusters:      []string{"cluster-a", "cluster-b"},
			IdleThresholdMinutes: 45,
		},
	}
	if _, err := s.UpdateConfig(ctx, in); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got, err := s.LoadConfig(ctx)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.DailyReport != in.DailyReport {
		t.Errorf("DailyReport = %+v, want %+v", got.DailyReport, in.DailyReport)
	}
	if got.IdleAlert.Enabled != in.IdleAlert.Enabled ||
		got.IdleAlert.IdleThresholdMinutes != in.IdleAlert.IdleThresholdMinutes ||
		len(got.IdleAlert.WatchedClusters) != len(in.IdleAlert.WatchedClusters) {
		t.Errorf("IdleAlert = %+v, want %+v", got.IdleAlert, in.IdleAlert)
	}
}

func TestArmThenDisarmIdleAlert(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	armed, err := s.ArmIdleAlert(ctx)
	if err != nil {
		t.Fatalf("ArmIdleAlert: %v", err)
	}
	if !armed.IdleAlert.Armed || armed.IdleAlert.ArmedAt == nil {
		t.Fatalf("expected armed=true with ArmedAt set, got %+v", armed.IdleAlert)
	}

	disarmed, err := s.DisarmIdleAlert(ctx)
	if err != nil {
		t.Fatalf("DisarmIdleAlert: %v", err)
	}
	if disarmed.IdleAlert.Armed || disarmed.IdleAlert.ArmedAt != nil {
		t.Fatalf("expected armed=false with ArmedAt nil, got %+v", disarmed.IdleAlert)
	}
}

func TestAppendHistoryCapsLength(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	for range maxHistoryLen + 10 {
		s.appendHistory(ctx, HistoryEntry{Type: HistoryTypeDailyReport, Result: ResultSuccess})
	}

	entries, err := s.LoadHistory(ctx)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != maxHistoryLen {
		t.Errorf("len(entries) = %d, want %d", len(entries), maxHistoryLen)
	}
}
