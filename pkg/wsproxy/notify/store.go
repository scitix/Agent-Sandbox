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

// Package notify implements the Hub's Prometheus-backed daily report and
// idle-alert notification service, posted to Feishu and configured via the
// admin API. Config and runtime state (history, arm/disarm) persist in a
// single ConfigMap so ws-proxy restarts don't lose them.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

const (
	configDataKey  = "config.json"
	historyDataKey = "history.json"
	maxHistoryLen  = 50

	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "agentbox-dashboard"
)

// Result / history type constants — shared between the scheduler and the
// admin API handlers.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"

	HistoryTypeDailyReport = "dailyReport"
	HistoryTypeIdleAlert   = "idleAlert"
)

// DailyReportConfig controls the scheduled daily Feishu report.
type DailyReportConfig struct {
	Enabled bool `json:"enabled"`
	// SendHourCST is the hour of day (0-23) in Asia/Shanghai the report is sent.
	SendHourCST int `json:"sendHourCST"`
}

// IdleAlertConfig controls the idle-cluster alert.
type IdleAlertConfig struct {
	Enabled bool `json:"enabled"`
	// WatchedClusters are cluster IDs monitored for the idle condition. The
	// alert fires only when ALL of them are simultaneously idle.
	WatchedClusters []string `json:"watchedClusters"`
	// IdleThresholdMinutes is the minutes of zero sandbox-create activity
	// (any result) required to consider a cluster idle.
	IdleThresholdMinutes int `json:"idleThresholdMinutes"`
	// Armed is whether idle detection is currently active. Auto-disarmed
	// after firing once.
	Armed   bool       `json:"armed"`
	ArmedAt *time.Time `json:"armedAt,omitempty"`
}

// Config is the persisted notification configuration.
type Config struct {
	DailyReport DailyReportConfig `json:"dailyReport"`
	IdleAlert   IdleAlertConfig   `json:"idleAlert"`
}

func defaultConfig() Config {
	return Config{
		DailyReport: DailyReportConfig{Enabled: true, SendHourCST: 9},
		IdleAlert: IdleAlertConfig{
			Enabled:              false,
			WatchedClusters:      []string{},
			IdleThresholdMinutes: 30,
		},
	}
}

// HistoryEntry records the outcome of one daily-report or idle-alert run.
type HistoryEntry struct {
	Time time.Time `json:"time"`
	Type string    `json:"type"`
	// Result is "success" or "failure".
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// Params configures a new Service.
type Params struct {
	Client           client.Client
	Namespace        string
	ConfigMapName    string
	PrometheusURL    string
	PrometheusToken  string
	FeishuWebhookURL string
	Clusters         *cluster.Store
}

// Service is the notification service: it owns the persisted config +
// history ConfigMap, the Prometheus query engine, and the Feishu sender, and
// runs the daily-report and idle-alert background loops.
type Service struct {
	k8sClient client.Client
	namespace string
	name      string

	prometheusURL   string
	prometheusToken string
	feishuWebhook   string

	clusters *cluster.Store

	// mu serializes read-modify-write config updates (admin API calls and
	// the idle-alert auto-disarm can race otherwise).
	mu sync.Mutex
}

// New constructs a Service. It performs no I/O.
func New(p Params) *Service {
	return &Service{
		k8sClient:       p.Client,
		namespace:       p.Namespace,
		name:            p.ConfigMapName,
		prometheusURL:   p.PrometheusURL,
		prometheusToken: p.PrometheusToken,
		feishuWebhook:   p.FeishuWebhookURL,
		clusters:        p.Clusters,
	}
}

// Enabled reports whether the service has enough configuration to do
// anything: a K8s client to persist state, and a metrics source to read.
func (s *Service) Enabled() bool {
	return s != nil && s.k8sClient != nil && s.prometheusURL != ""
}

func (s *Service) get(ctx context.Context) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	err := s.k8sClient.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: s.name}, cm)
	return cm, err
}

// ensureConfigMap bootstraps the ConfigMap with default config if it does not
// exist yet. It never overwrites an existing ConfigMap.
func (s *Service) ensureConfigMap(ctx context.Context) {
	if s.k8sClient == nil {
		return
	}
	_, err := s.get(ctx)
	if err == nil {
		return
	}
	if !apierrors.IsNotFound(err) {
		log.Printf("wsproxy: notify: ensure configmap get error: %v", err)
		return
	}

	rawCfg, _ := json.Marshal(defaultConfig())
	newCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.name,
			Namespace: s.namespace,
			Labels:    map[string]string{managedByLabel: managedByValue},
		},
		Data: map[string]string{
			configDataKey:  string(rawCfg),
			historyDataKey: "[]",
		},
	}
	if err := s.k8sClient.Create(ctx, newCM); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Printf("wsproxy: notify: ensure configmap create error: %v", err)
		return
	}
	log.Printf("wsproxy: notify: bootstrapped notification ConfigMap %s/%s", s.namespace, s.name)
}

// LoadConfig returns the persisted config, or the default config if the
// ConfigMap (or the key within it) does not exist yet.
func (s *Service) LoadConfig(ctx context.Context) (Config, error) {
	if s.k8sClient == nil {
		return defaultConfig(), nil
	}
	cm, err := s.get(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return defaultConfig(), nil
		}
		return Config{}, err
	}
	raw, ok := cm.Data[configDataKey]
	if !ok || raw == "" {
		return defaultConfig(), nil
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse notification config: %w", err)
	}
	return cfg, nil
}

// saveConfig writes cfg to the ConfigMap, creating it if necessary. Callers
// that need read-modify-write atomicity must hold s.mu.
func (s *Service) saveConfig(ctx context.Context, cfg Config) error {
	if s.k8sClient == nil {
		return fmt.Errorf("notification service has no k8s client configured")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	cm, err := s.get(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.namespace,
				Labels:    map[string]string{managedByLabel: managedByValue},
			},
			Data: map[string]string{configDataKey: string(raw), historyDataKey: "[]"},
		}
		return s.k8sClient.Create(ctx, newCM)
	}
	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[configDataKey] = string(raw)
	return s.k8sClient.Update(ctx, updated)
}

// UpdateConfig replaces the persisted config wholesale (PUT semantics) and
// returns what was actually stored.
func (s *Service) UpdateConfig(ctx context.Context, cfg Config) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveConfig(ctx, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadHistory returns the persisted history, most-recent last.
func (s *Service) LoadHistory(ctx context.Context) ([]HistoryEntry, error) {
	if s.k8sClient == nil {
		return []HistoryEntry{}, nil
	}
	cm, err := s.get(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return []HistoryEntry{}, nil
		}
		return nil, err
	}
	raw, ok := cm.Data[historyDataKey]
	if !ok || raw == "" {
		return []HistoryEntry{}, nil
	}
	var entries []HistoryEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse notification history: %w", err)
	}
	return entries, nil
}

// appendHistory records one run outcome, capping retained history at
// maxHistoryLen so the ConfigMap does not grow unbounded.
func (s *Service) appendHistory(ctx context.Context, entry HistoryEntry) {
	if s.k8sClient == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.LoadHistory(ctx)
	if err != nil {
		log.Printf("wsproxy: notify: load history before append failed: %v", err)
		entries = nil
	}
	entries = append(entries, entry)
	if len(entries) > maxHistoryLen {
		entries = entries[len(entries)-maxHistoryLen:]
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		log.Printf("wsproxy: notify: marshal history failed: %v", err)
		return
	}

	cm, err := s.get(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			log.Printf("wsproxy: notify: get configmap before history append failed: %v", err)
			return
		}
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.namespace,
				Labels:    map[string]string{managedByLabel: managedByValue},
			},
			Data: map[string]string{historyDataKey: string(raw)},
		}
		if err := s.k8sClient.Create(ctx, newCM); err != nil {
			log.Printf("wsproxy: notify: create configmap for history append failed: %v", err)
		}
		return
	}
	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[historyDataKey] = string(raw)
	if err := s.k8sClient.Update(ctx, updated); err != nil {
		log.Printf("wsproxy: notify: update configmap for history append failed: %v", err)
	}
}

// ArmIdleAlert marks the idle alert as armed and returns the updated config.
func (s *Service) ArmIdleAlert(ctx context.Context) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	now := time.Now().UTC()
	cfg.IdleAlert.Armed = true
	cfg.IdleAlert.ArmedAt = &now
	if err := s.saveConfig(ctx, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// DisarmIdleAlert marks the idle alert as disarmed and returns the updated
// config.
func (s *Service) DisarmIdleAlert(ctx context.Context) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	cfg.IdleAlert.Armed = false
	cfg.IdleAlert.ArmedAt = nil
	if err := s.saveConfig(ctx, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
