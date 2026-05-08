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
	"regexp"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// IAMResolveResult holds the resolved identity information for an IAM user.
type IAMResolveResult struct {
	// Username is the normalised username.
	Username string
	// Team is the team derived from ScitixQuota labels. Empty when no quota found.
	Team string
	// Namespace is the effective Kubernetes namespace to use for this user.
	// When the computed namespace (t-{team}-{username}) exists in the cluster it
	// equals that value; otherwise it falls back to "default".
	Namespace string
	// QuotaURL is the quota URL label value, if found.
	QuotaURL string
}

// IAMService defines operations related to IAM identity resolution.
type IAMService interface {
	// ResolveNamespace resolves the effective namespace for a given team+user pair.
	// Results are cached permanently (until process restart) for performance.
	ResolveNamespace(ctx context.Context, team, user string) (string, *domain.AppError)
}

type k8sIAMService struct {
	client client.Client
	// nsCache caches resolved namespace values keyed by "team/user".
	// Permanent cache — entries are never evicted until process restart.
	nsCache   map[string]string
	nsCacheMu sync.RWMutex
}

// NewIAMService creates an IAMService backed by Kubernetes ScitixQuota CRs.
func NewIAMService(c client.Client) IAMService {
	return &k8sIAMService{
		client:  c,
		nsCache: make(map[string]string),
	}
}

var nonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]`)

// sanitizeNamePart lowercases a string and replaces disallowed characters with '-'.
func sanitizeNamePart(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumDash.ReplaceAllString(s, "-")
	// Trim leading/trailing dashes.
	s = strings.Trim(s, "-")
	if s == "" {
		s = "unknown"
	}
	return s
}

// buildNamespace computes the Kubernetes namespace from team and username.
func buildNamespace(team, username string) string {
	if team == "" {
		team = "default"
	}
	return fmt.Sprintf("t-%s-%s", sanitizeNamePart(team), sanitizeNamePart(username))
}

// ResolveNamespace resolves the effective namespace for a given team+user pair.
// It checks the in-memory cache first; on miss it calls ResolveUser and writes
// the result to the cache.
func (s *k8sIAMService) ResolveNamespace(ctx context.Context, team, user string) (string, *domain.AppError) {
	cacheKey := team + "/" + user

	// Fast path: read lock.
	s.nsCacheMu.RLock()
	if ns, ok := s.nsCache[cacheKey]; ok {
		s.nsCacheMu.RUnlock()
		return ns, nil
	}
	s.nsCacheMu.RUnlock()

	// Slow path: resolve and write.
	ns := s.resolveNamespaceFromK8s(ctx, buildNamespace(team, user))

	s.nsCacheMu.Lock()
	s.nsCache[cacheKey] = ns
	s.nsCacheMu.Unlock()

	return ns, nil
}

// resolveNamespaceFromK8s returns ns if it exists in the cluster, otherwise "default".
func (s *k8sIAMService) resolveNamespaceFromK8s(ctx context.Context, ns string) string {
	obj := &corev1.Namespace{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: ns}, obj); err != nil {
		if k8serrors.IsNotFound(err) {
			return "default"
		}
		// On unexpected error be conservative and return default to avoid 404s.
		return "default"
	}
	return ns
}
