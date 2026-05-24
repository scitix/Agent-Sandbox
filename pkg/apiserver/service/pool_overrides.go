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
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// renderOptionsFromGen projects the caller-supplied gen.PoolTemplateOverrides
// (wire shape used by the legacy Pool create API) into sandboxrender.Options.
// Image-only — per-Pool resource sizing flows through
// EnvClusterMember.InlineResources now.
func renderOptionsFromGen(g *gen.PoolTemplateOverrides) sandboxrender.Options {
	if g == nil {
		return sandboxrender.Options{}
	}
	out := sandboxrender.Options{}
	if g.Image != nil {
		out.Image = *g.Image
	}
	return out
}
