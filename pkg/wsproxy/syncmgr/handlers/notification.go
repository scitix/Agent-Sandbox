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
	"log"
	"time"

	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/notify"
)

func notificationConfigToGen(cfg notify.Config) wsproxygen.NotificationConfig {
	return wsproxygen.NotificationConfig{
		DailyReport: wsproxygen.DailyReportConfig{
			Enabled:     cfg.DailyReport.Enabled,
			SendHourCST: cfg.DailyReport.SendHourCST,
		},
		IdleAlert: wsproxygen.IdleAlertConfig{
			Enabled:              cfg.IdleAlert.Enabled,
			WatchedClusters:      cfg.IdleAlert.WatchedClusters,
			IdleThresholdMinutes: cfg.IdleAlert.IdleThresholdMinutes,
			Armed:                cfg.IdleAlert.Armed,
			ArmedAt:              cfg.IdleAlert.ArmedAt,
		},
	}
}

func notificationConfigFromGen(cfg wsproxygen.NotificationConfig) notify.Config {
	return notify.Config{
		DailyReport: notify.DailyReportConfig{
			Enabled:     cfg.DailyReport.Enabled,
			SendHourCST: cfg.DailyReport.SendHourCST,
		},
		IdleAlert: notify.IdleAlertConfig{
			Enabled:              cfg.IdleAlert.Enabled,
			WatchedClusters:      cfg.IdleAlert.WatchedClusters,
			IdleThresholdMinutes: cfg.IdleAlert.IdleThresholdMinutes,
			Armed:                cfg.IdleAlert.Armed,
			ArmedAt:              cfg.IdleAlert.ArmedAt,
		},
	}
}

// ── AdminGetNotificationConfig ───────────────────────────────────────────────

func (s *Server) AdminGetNotificationConfig(
	ctx context.Context,
	_ wsproxygen.AdminGetNotificationConfigRequestObject,
) (wsproxygen.AdminGetNotificationConfigResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminGetNotificationConfig503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminGetNotificationConfig403JSONResponse{Error: "admin access required"}, nil
	}

	cfg, err := s.notify.LoadConfig(ctx)
	if err != nil {
		log.Printf("syncManager: notification get config error: %v", err)
		return wsproxygen.AdminGetNotificationConfig503JSONResponse{Error: "failed to load notification config"}, nil
	}
	return wsproxygen.AdminGetNotificationConfig200JSONResponse(notificationConfigToGen(cfg)), nil
}

// ── AdminUpdateNotificationConfig ────────────────────────────────────────────

func (s *Server) AdminUpdateNotificationConfig(
	ctx context.Context,
	request wsproxygen.AdminUpdateNotificationConfigRequestObject,
) (wsproxygen.AdminUpdateNotificationConfigResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminUpdateNotificationConfig503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminUpdateNotificationConfig403JSONResponse{Error: "admin access required"}, nil
	}
	if request.Body == nil {
		return wsproxygen.AdminUpdateNotificationConfig400JSONResponse{Error: "request body required"}, nil
	}

	updated, err := s.notify.UpdateConfig(ctx, notificationConfigFromGen(*request.Body))
	if err != nil {
		log.Printf("syncManager: notification update config error: %v", err)
		return wsproxygen.AdminUpdateNotificationConfig503JSONResponse{Error: "failed to save notification config"}, nil
	}
	return wsproxygen.AdminUpdateNotificationConfig200JSONResponse(notificationConfigToGen(updated)), nil
}

// ── AdminGetNotificationHistory ──────────────────────────────────────────────

func (s *Server) AdminGetNotificationHistory(
	ctx context.Context,
	_ wsproxygen.AdminGetNotificationHistoryRequestObject,
) (wsproxygen.AdminGetNotificationHistoryResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminGetNotificationHistory503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminGetNotificationHistory403JSONResponse{Error: "admin access required"}, nil
	}

	entries, err := s.notify.LoadHistory(ctx)
	if err != nil {
		log.Printf("syncManager: notification get history error: %v", err)
		return wsproxygen.AdminGetNotificationHistory503JSONResponse{Error: "failed to load notification history"}, nil
	}

	items := make([]wsproxygen.NotificationHistoryEntry, len(entries))
	for i, e := range entries {
		items[i] = wsproxygen.NotificationHistoryEntry{
			Time:   e.Time,
			Type:   wsproxygen.NotificationHistoryEntryType(e.Type),
			Result: wsproxygen.NotificationHistoryEntryResult(e.Result),
		}
		if e.Detail != "" {
			items[i].Detail = &e.Detail
		}
	}
	return wsproxygen.AdminGetNotificationHistory200JSONResponse{Entries: items}, nil
}

// ── AdminTriggerDailyReport ───────────────────────────────────────────────────

func (s *Server) AdminTriggerDailyReport(
	ctx context.Context,
	_ wsproxygen.AdminTriggerDailyReportRequestObject,
) (wsproxygen.AdminTriggerDailyReportResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminTriggerDailyReport503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminTriggerDailyReport403JSONResponse{Error: "admin access required"}, nil
	}

	s.notify.SendDailyReportNow(ctx, time.Now())
	return wsproxygen.AdminTriggerDailyReport200JSONResponse{
		Result: wsproxygen.DailyReportTriggerResultResultSuccess,
	}, nil
}

// ── AdminArmIdleAlert ─────────────────────────────────────────────────────────

func (s *Server) AdminArmIdleAlert(
	ctx context.Context,
	_ wsproxygen.AdminArmIdleAlertRequestObject,
) (wsproxygen.AdminArmIdleAlertResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminArmIdleAlert503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminArmIdleAlert403JSONResponse{Error: "admin access required"}, nil
	}

	cfg, err := s.notify.ArmIdleAlert(ctx)
	if err != nil {
		log.Printf("syncManager: notification arm idle alert error: %v", err)
		return wsproxygen.AdminArmIdleAlert503JSONResponse{Error: "failed to arm idle alert"}, nil
	}
	return wsproxygen.AdminArmIdleAlert200JSONResponse(notificationConfigToGen(cfg).IdleAlert), nil
}

// ── AdminDisarmIdleAlert ──────────────────────────────────────────────────────

func (s *Server) AdminDisarmIdleAlert(
	ctx context.Context,
	_ wsproxygen.AdminDisarmIdleAlertRequestObject,
) (wsproxygen.AdminDisarmIdleAlertResponseObject, error) {
	if s.notify == nil {
		return wsproxygen.AdminDisarmIdleAlert503JSONResponse{Error: "notification service not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminDisarmIdleAlert403JSONResponse{Error: "admin access required"}, nil
	}

	cfg, err := s.notify.DisarmIdleAlert(ctx)
	if err != nil {
		log.Printf("syncManager: notification disarm idle alert error: %v", err)
		return wsproxygen.AdminDisarmIdleAlert503JSONResponse{Error: "failed to disarm idle alert"}, nil
	}
	return wsproxygen.AdminDisarmIdleAlert200JSONResponse(notificationConfigToGen(cfg).IdleAlert), nil
}
