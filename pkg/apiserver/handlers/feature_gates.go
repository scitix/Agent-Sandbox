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

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// GetFeatureGates reports which optional features are active on this
// deployment. The response drives dashboard feature toggles (show the
// quota selector?) and lets SDK callers skip feature-specific calls when
// the corresponding provider is disabled.
//
// Each gate is a boolean: true when a non-noop provider is wired in, false
// when the feature is absent from this build/deployment. Callers should not
// depend on which concrete provider is installed — only whether the feature
// is available.
func (s *Server) GetFeatureGates(_ context.Context, _ gen.GetFeatureGatesRequestObject) (gen.GetFeatureGatesResponseObject, error) {
	return gen.GetFeatureGates200JSONResponse{
		Quota: s.quotaProvider.Enabled(),
	}, nil
}
