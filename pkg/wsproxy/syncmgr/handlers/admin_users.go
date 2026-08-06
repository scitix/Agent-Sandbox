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
	"sort"
	"time"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
)

// dedupUsersByTeam deduplicates key metadata by (Team, User) — including
// entries whose ExpiresAt has already passed, since a lapsed key still
// counts as a registered platform user for this report — and returns the
// total distinct user count plus a per-team distinct user count.
func dedupUsersByTeam(metas []apikey.KeyMetadata) (total int, byTeam map[string]int) {
	seen := make(map[string]struct{}, len(metas))
	teamSeen := make(map[string]map[string]struct{}, len(metas))
	for _, m := range metas {
		key := m.Team + "|" + m.User
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		users, ok := teamSeen[m.Team]
		if !ok {
			users = make(map[string]struct{})
			teamSeen[m.Team] = users
		}
		users[m.User] = struct{}{}
	}

	byTeam = make(map[string]int, len(teamSeen))
	for team, users := range teamSeen {
		byTeam[team] = len(users)
	}
	return len(seen), byTeam
}

// ── PlatformUsersCount ────────────────────────────────────────────────────────

func (s *Server) PlatformUsersCount(
	ctx context.Context,
	_ wsproxygen.PlatformUsersCountRequestObject,
) (wsproxygen.PlatformUsersCountResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.KeyStore == nil {
		return wsproxygen.PlatformUsersCount503JSONResponse{Error: "key store not configured"}, nil
	}

	metas, err := deps.KeyStore.List(ctx)
	if err != nil {
		log.Printf("syncManager: platform users count error: %v", err)
		return wsproxygen.PlatformUsersCount503JSONResponse{Error: "failed to list keys"}, nil
	}

	total, _ := dedupUsersByTeam(metas)
	return wsproxygen.PlatformUsersCount200JSONResponse{
		TotalUsers:  total,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// ── AdminUsersSummary ─────────────────────────────────────────────────────────

func (s *Server) AdminUsersSummary(
	ctx context.Context,
	_ wsproxygen.AdminUsersSummaryRequestObject,
) (wsproxygen.AdminUsersSummaryResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.KeyStore == nil {
		return wsproxygen.AdminUsersSummary503JSONResponse{Error: "key store not configured"}, nil
	}
	if !s.requireAdmin(ctx) {
		return wsproxygen.AdminUsersSummary403JSONResponse{Error: "admin access required"}, nil
	}

	metas, err := deps.KeyStore.List(ctx)
	if err != nil {
		log.Printf("syncManager: admin users summary error: %v", err)
		return wsproxygen.AdminUsersSummary503JSONResponse{Error: "failed to list keys"}, nil
	}

	total, byTeamCounts := dedupUsersByTeam(metas)

	byTeam := make([]wsproxygen.AdminUsersSummaryTeam, 0, len(byTeamCounts))
	for team, users := range byTeamCounts {
		byTeam = append(byTeam, wsproxygen.AdminUsersSummaryTeam{Team: team, Users: users})
	}
	sort.Slice(byTeam, func(i, j int) bool { return byTeam[i].Team < byTeam[j].Team })

	return wsproxygen.AdminUsersSummary200JSONResponse{
		TotalUsers:  total,
		ByTeam:      byTeam,
		GeneratedAt: time.Now().UTC(),
	}, nil
}
