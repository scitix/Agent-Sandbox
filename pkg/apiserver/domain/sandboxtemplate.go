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

package domain

// SandboxTemplate is the domain model for a SandboxTemplate CRD.
type SandboxTemplate struct {
	Name         string
	Version      string
	Description  string
	RuntimeNames []string
	// CPU is the sum of all containers' CPU requests (raw K8s string, e.g. "8000m").
	CPU string
	// Memory is the sum of all containers' memory requests (raw K8s string, e.g. "8Gi").
	Memory string
	// CreatedAt is the RFC3339 creation time of the template.
	CreatedAt string
	// SyncSource is "global" when the template was created/synced via ws-proxy.
	SyncSource string
	// Docs is the Markdown documentation content from the agentbox.navix.sh/docs annotation.
	Docs string
	// CrdYaml is the complete raw CRD YAML without managedFields; includes resourceVersion for optimistic locking.
	CrdYaml string
}
