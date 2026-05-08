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

// ClusterSummary describes one cluster visible to the gateway's routing table.
// It is intentionally minimal: the full per-plane URLs and headers live in the
// private cluster config and should never be exposed through the public API.
type ClusterSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Local bool   `json:"local"`
}

// ListClustersResult is returned by ClusterService.List and wraps the catalog
// so future fields (e.g. gateway health) can be added without breaking clients.
type ListClustersResult struct {
	Clusters []ClusterSummary `json:"clusters"`
}
