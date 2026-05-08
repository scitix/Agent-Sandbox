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
	"strings"
	"time"

	"k8s.io/utils/ptr"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// TemplateToGen converts a domain.SandboxTemplate to the generated gen.SandboxTemplate type.
// Shared between the API Server handlers and the WsProxy internal HTTP API so that both
// return an identical response shape.
func TemplateToGen(t *domain.SandboxTemplate) gen.SandboxTemplate {
	tmpl := gen.SandboxTemplate{
		Name:        t.Name,
		Version:     ptr.To(t.Version),
		Description: ptr.To(t.Description),
		Cpu:         ptr.To(t.CPU),
		Memory:      ptr.To(t.Memory),
		SyncSource:  ptr.To(t.SyncSource),
		Docs:        ptr.To(t.Docs),
		CrdYaml:     ptr.To(t.CrdYaml),
	}
	if t.CreatedAt != "" {
		if ts, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
			tmpl.CreatedAt = &ts
		}
	}
	return tmpl
}

// TemplateToSummaryGen converts a domain.SandboxTemplate to the lightweight
// gen.SandboxTemplateSummary type (omits docs/crdYaml).
func TemplateToSummaryGen(t *domain.SandboxTemplate) gen.SandboxTemplateSummary {
	summary := gen.SandboxTemplateSummary{
		Name:        t.Name,
		Version:     ptr.To(t.Version),
		Description: ptr.To(t.Description),
		Cpu:         ptr.To(t.CPU),
		Memory:      ptr.To(t.Memory),
		SyncSource:  ptr.To(t.SyncSource),
	}
	if t.CreatedAt != "" {
		if ts, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
			summary.CreatedAt = &ts
		}
	}
	if len(t.RuntimeNames) > 0 {
		names := make([]string, len(t.RuntimeNames))
		copy(names, t.RuntimeNames)
		summary.RuntimeNames = &names
	}
	hasDocs := strings.TrimSpace(t.Docs) != ""
	summary.HasDocs = &hasDocs
	return summary
}
