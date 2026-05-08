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

package cluster

// SplitSandboxID extracts the cluster ID prefix from a sandbox ID.
// Input format: "{clusterID}.{uuid}" or just "{uuid}".
// Returns (clusterID, rawID). If no prefix is present, clusterID is empty.
func SplitSandboxID(id string) (clusterID, rawID string) {
	if len(id) > 36 && id[len(id)-37] == '.' {
		return id[:len(id)-37], id[len(id)-36:]
	}
	return "", id
}
