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

import "strings"

// ParsedPoolRef holds the result of parsing a pool reference string.
type ParsedPoolRef struct {
	ClusterID     string // empty when no "clusterID::" prefix is present
	PoolName      string // the K8s resource name of the SandboxPool
	ImageOverride string // empty when no "//imageOverride" suffix is present
}

// ParsePoolRef parses an input string of the form [clusterID::]poolName[//imageOverride].
//
// For the native API, callers pass "[clusterID::]poolName" (no image override).
// For the E2B API, callers pass "[clusterID::]poolName[//imageOverride]".
func ParsePoolRef(raw string) ParsedPoolRef {
	var ref ParsedPoolRef

	// Split image override first — "//" can appear in the image portion.
	prefix := raw
	if before, after, ok := strings.Cut(raw, "//"); ok {
		prefix = before
		ref.ImageOverride = strings.TrimSpace(after)
	}

	// Split cluster ID prefix.
	if before, after, ok := strings.Cut(prefix, "::"); ok {
		ref.ClusterID = strings.TrimSpace(before)
		ref.PoolName = strings.TrimSpace(after)
	} else {
		ref.PoolName = strings.TrimSpace(prefix)
	}

	return ref
}
