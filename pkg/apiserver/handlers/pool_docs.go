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
	"strings"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// renderPoolDocs substitutes the ${AGBX_POOL_NAME}, ${AGBX_CLUSTER_ID}, ${AGBX_API_KEY}
// placeholders in the raw pool docs markdown with real values. The API key is resolved from
// the acting user's API keys: the first entry whose RawToken is non-empty wins
// (legacy keys with only a hash stored are skipped because users cannot run the
// rendered snippets without the plaintext token).
//
// When raw is empty, returns ("", nil) — nothing to render.
// When raw contains ${AGBX_API_KEY} but no usable key is found, returns
// ("", APIKeyRequired AppError) so the caller can surface it to the user.
func (s *Server) renderPoolDocs(ctx context.Context, raw, poolName, clusterID string, auth domain.AuthInfo) (string, *domain.AppError) {
	if raw == "" {
		return "", nil
	}

	needsKey := strings.Contains(raw, "${AGBX_API_KEY}")

	var apiKeyValue string
	if needsKey {
		keys, appErr := s.apikey.ListByTeamAndUser(ctx, auth.Team, auth.User)
		if appErr != nil {
			return "", appErr
		}
		if keys != nil {
			for _, k := range keys.Items {
				if k.RawToken != "" {
					apiKeyValue = k.RawToken
					break
				}
			}
		}
		if apiKeyValue == "" {
			return "", domain.NewAPIKeyRequired(
				"no API key with a recoverable token found for this user; please create a new API key on the API Keys page to view the pool docs",
			)
		}
	}

	rendered := raw
	rendered = strings.ReplaceAll(rendered, "${AGBX_POOL_NAME}", poolName)
	rendered = strings.ReplaceAll(rendered, "${AGBX_CLUSTER_ID}", clusterID)
	rendered = strings.ReplaceAll(rendered, "${AGBX_API_KEY}", apiKeyValue)
	return rendered, nil
}

// renderTemplateDocs substitutes the ${AGBX_POOL_NAME}, ${AGBX_CLUSTER_ID}, ${AGBX_API_KEY}
// placeholders with human-readable display hints so that the template docs page gives
// users a concrete preview of what the rendered snippets will look like.
// ${AGBX_CLUSTER_ID} is substituted with the real cluster ID; the other two become
// placeholder strings (YOUR_POOL_NAME, YOUR_API_KEY).
func renderTemplateDocs(raw, clusterID string) string {
	if raw == "" {
		return ""
	}
	rendered := raw
	rendered = strings.ReplaceAll(rendered, "${AGBX_POOL_NAME}", "YOUR_POOL_NAME")
	rendered = strings.ReplaceAll(rendered, "${AGBX_CLUSTER_ID}", clusterID)
	rendered = strings.ReplaceAll(rendered, "${AGBX_API_KEY}", "YOUR_API_KEY")
	return rendered
}
