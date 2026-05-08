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

package service

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// OrganizationService defines operations for querying organizational structure
// (teams, users, namespaces) derived from API Key Secret labels.
type OrganizationService interface {
	// ListTeams returns all unique team names found across API Key secrets.
	ListTeams(ctx context.Context) ([]string, *domain.AppError)

	// ListUsersByTeam returns all unique user names belonging to the given team,
	// derived from API Key secrets filtered by team label.
	ListUsersByTeam(ctx context.Context, team string) ([]string, *domain.AppError)

	// ListNamespaces returns all Kubernetes namespace names visible to the operator.
	ListNamespaces(ctx context.Context) ([]string, *domain.AppError)
}

type k8sOrganizationService struct {
	client   client.Client
	keyStore apikey.KeyStore
}

// NewOrganizationService creates an OrganizationService backed by Kubernetes resources.
// keyStore is used to enumerate teams and users via API Key Secret labels.
func NewOrganizationService(c client.Client, ks apikey.KeyStore) OrganizationService {
	return &k8sOrganizationService{
		client:   c,
		keyStore: ks,
	}
}

// ListTeams returns all unique team names across all API Key secrets.
// Only users with at least one API Key are surfaced here.
func (s *k8sOrganizationService) ListTeams(ctx context.Context) ([]string, *domain.AppError) {
	metas, err := s.keyStore.List(ctx)
	if err != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to list API key secrets: %v", err), err)
	}

	seen := make(map[string]struct{}, len(metas))
	for _, m := range metas {
		if m.Team != "" {
			seen[m.Team] = struct{}{}
		}
	}

	teams := make([]string, 0, len(seen))
	for t := range seen {
		teams = append(teams, t)
	}
	sort.Strings(teams)
	return teams, nil
}

// ListUsersByTeam returns all unique user names for the given team, derived from
// API Key secrets filtered by team label.
func (s *k8sOrganizationService) ListUsersByTeam(ctx context.Context, team string) ([]string, *domain.AppError) {
	if team == "" {
		return nil, domain.NewBadRequest("team name must not be empty")
	}

	metas, err := s.keyStore.ListByTeamAndUser(ctx, team, "")
	if err != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to list API key secrets: %v", err), err)
	}

	seen := make(map[string]struct{}, len(metas))
	for _, m := range metas {
		if m.User != "" {
			seen[m.User] = struct{}{}
		}
	}

	users := make([]string, 0, len(seen))
	for u := range seen {
		users = append(users, u)
	}
	sort.Strings(users)
	return users, nil
}

// ListNamespaces lists all Kubernetes namespace names.
func (s *k8sOrganizationService) ListNamespaces(ctx context.Context) ([]string, *domain.AppError) {
	nsList := &corev1.NamespaceList{}
	if err := s.client.List(ctx, nsList); err != nil {
		return nil, domain.NewInternal(fmt.Sprintf("failed to list namespaces: %v", err), err)
	}

	names := make([]string, 0, len(nsList.Items))
	for i := range nsList.Items {
		names = append(names, nsList.Items[i].Name)
	}
	sort.Strings(names)
	return names, nil
}
