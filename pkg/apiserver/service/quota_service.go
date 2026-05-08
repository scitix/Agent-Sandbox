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

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// QuotaService defines operations for querying user-visible quotas.
//
// The concrete behaviour (ScitixQuota CRD, external API, disabled stub, ...)
// is selected by the quota.Provider injected at construction time. The
// service layer itself stays vendor-neutral — see pkg/plugins/quota for the
// provider contract and pkg/scitix/quota for the Scitix implementation.
type QuotaService interface {
	// ListForUser returns all quotas matching the given user and team. When
	// the underlying provider is disabled the result is an empty list, not
	// an error — callers should treat that as "feature unavailable".
	ListForUser(ctx context.Context, user, team string) ([]domain.QuotaInfo, *domain.AppError)
}

type providerBackedQuotaService struct {
	p quotaplugin.Provider
}

// NewQuotaServiceFromProvider wraps a quota.Provider in the service-level
// interface consumed by HTTP handlers. The provider may be a Noop, which
// yields a service whose ListForUser always returns (nil, nil).
func NewQuotaServiceFromProvider(p quotaplugin.Provider) QuotaService {
	if p == nil {
		p = quotaplugin.NewNoop()
	}
	return &providerBackedQuotaService{p: p}
}

func (s *providerBackedQuotaService) ListForUser(ctx context.Context, user, team string) ([]domain.QuotaInfo, *domain.AppError) {
	return s.p.ListForUser(ctx, user, team)
}
