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
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// The registry-rewrite implementation lives in pkg/sandboxrender so the Pool
// renderer can rewrite template-owned images (idleImage, containers,
// initContainers) as well as the caller-supplied ones rewritten here at claim
// time. It cannot live in this package: pkg/controllers/.../poolrender calls the
// renderer, and this package depends on poolrender through envmember, so the
// reverse edge would be an import cycle.
//
// These aliases keep the claim-time call sites in sandbox_service.go unchanged.
type RegistryStore = sandboxrender.RegistryStore

// RewriteImageForCluster rewrites the registry host of image to the registry
// owned by currentClusterID. See sandboxrender.RewriteImageForCluster.
func RewriteImageForCluster(image, currentClusterID string, store RegistryStore) string {
	return sandboxrender.RewriteImageForCluster(image, currentClusterID, store)
}

// ensure cluster.Store satisfies RegistryStore at compile time. The assertion
// stays in this package so pkg/sandboxrender does not need to import
// pkg/utils/cluster.
var _ RegistryStore = (*cluster.Store)(nil)
