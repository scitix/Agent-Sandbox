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

package sandboxrender

import (
	"strings"

	"github.com/distribution/reference"
	"k8s.io/klog/v2"
)

// RegistryStore is the subset of cluster.Store used by RewriteImageForCluster.
// Extracted as an interface so tests can inject a lightweight fake without
// depending on the full Store implementation.
//
// This interface lives here rather than in the apiserver service package so the
// renderer can rewrite template-owned images without the renderer's callers
// (pkg/controllers/...) taking a dependency on pkg/apiserver/service — that
// direction is an import cycle, since the service package depends on the
// renderer through envmember/poolrender.
type RegistryStore interface {
	LookupRegistry(host string) (clusterID, typ string, ok bool)
	RegistryForType(clusterID, typ string) (host string, ok bool)
}

// RegistryRewrite carries everything needed to rewrite an image to the local
// cluster's registry. A nil *RegistryRewrite means "do not rewrite".
type RegistryRewrite struct {
	// LocalClusterID is the cluster the rendered Pool will run in. Empty
	// disables rewriting (the operator was started without --local-cluster-id).
	LocalClusterID string
	// Store resolves registry hosts to owning clusters and back. Nil disables
	// rewriting.
	Store RegistryStore
}

// Rewrite returns image rewritten to the local cluster's registry, or image
// unchanged when r is nil or the rewrite does not apply. Safe on a nil receiver
// so callers can hold an unconditional *RegistryRewrite field.
func (r *RegistryRewrite) Rewrite(image string) string {
	if r == nil {
		return image
	}
	return RewriteImageForCluster(image, r.LocalClusterID, r.Store)
}

// RewriteImageForCluster rewrites the registry host of image when the image
// belongs to a private registry owned by a different cluster.
//
// Rewrite rules:
//  1. Parse the image reference to extract its registry host.
//  2. Look up the host in the store. If not found → public registry, return as-is.
//  3. If the owning cluster equals currentClusterID → already local, return as-is.
//  4. Find a registry of the same Type in currentClusterID. If none found → warn
//     and return as-is (never block the request).
//  5. Replace the registry host prefix and return the rewritten image.
//
// The operation is idempotent: after a successful rewrite the host belongs to
// currentClusterID, so rule 3 short-circuits a second call.
//
// Only the host prefix is replaced — repository path, tag and digest are
// preserved verbatim. Rewriting a public image into a mirror that inserts a
// path prefix is therefore out of scope (see RegistryEntry.Host, which forbids
// a path component).
func RewriteImageForCluster(image, currentClusterID string, store RegistryStore) string {
	if image == "" || currentClusterID == "" || store == nil {
		return image
	}

	host := registryHost(image)
	if host == "" {
		return image
	}

	ownerClusterID, typ, ok := store.LookupRegistry(host)
	if !ok {
		// Not a known private registry — leave unchanged.
		return image
	}
	if ownerClusterID == currentClusterID {
		// Already belongs to this cluster.
		return image
	}

	targetHost, ok := store.RegistryForType(currentClusterID, typ)
	if !ok {
		klog.V(2).InfoS("RewriteImageForCluster: no local registry for type, keeping original image",
			"image", image, "ownerCluster", ownerClusterID, "currentCluster", currentClusterID, "type", typ)
		return image
	}

	rewritten := replaceRegistryHost(image, host, targetHost)
	klog.V(2).InfoS("RewriteImageForCluster: rewrote image registry",
		"original", image, "rewritten", rewritten,
		"from", ownerClusterID, "to", currentClusterID)
	return rewritten
}

// RewriteSkipReason classifies why RewriteImageForCluster left an image
// unchanged. It exists so callers can distinguish the benign cases (a public
// image, or an image already local) from the one that will surface minutes
// later as ImagePullBackOff: the image belongs to a peer cluster's registry and
// this cluster has no counterpart to rewrite it to.
type RewriteSkipReason string

const (
	// RewriteApplied means the image was rewritten.
	RewriteApplied RewriteSkipReason = "applied"
	// RewriteSkipNotApplicable covers empty input, a disabled rewriter, an
	// unparseable reference, an implicit Docker Hub name, an unknown (public)
	// registry host, and an image already owned by the local cluster.
	RewriteSkipNotApplicable RewriteSkipReason = "not_applicable"
	// RewriteSkipNoLocalRegistry means the host is a known peer cluster's
	// registry but the local cluster declares no registry of the same Type.
	// The image is kept as-is and will be pulled cross-region, if at all.
	RewriteSkipNoLocalRegistry RewriteSkipReason = "no_local_registry"
)

// ClassifyRewrite reports what RewriteImageForCluster would do with image,
// without performing the rewrite. Used for metrics and for warning events.
func ClassifyRewrite(image, currentClusterID string, store RegistryStore) RewriteSkipReason {
	if image == "" || currentClusterID == "" || store == nil {
		return RewriteSkipNotApplicable
	}
	host := registryHost(image)
	if host == "" {
		return RewriteSkipNotApplicable
	}
	ownerClusterID, typ, ok := store.LookupRegistry(host)
	if !ok || ownerClusterID == currentClusterID {
		return RewriteSkipNotApplicable
	}
	if _, ok := store.RegistryForType(currentClusterID, typ); !ok {
		return RewriteSkipNoLocalRegistry
	}
	return RewriteApplied
}

// registryHost extracts the registry hostname (and optional port) from an OCI
// image reference string. Returns "" when the image cannot be parsed or when
// Docker Hub short names are used (no explicit host).
//
// We parse only to extract the host; we intentionally do not normalise the
// image string (e.g. adding "docker.io/" prefixes) so that the rewrite
// operation is a clean string substitution with no unintended side-effects.
func registryHost(image string) string {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return ""
	}
	host := reference.Domain(ref)
	// reference.ParseNormalizedNamed normalises docker.io short names by
	// prepending "docker.io". If the caller did not supply a host at all
	// (e.g. "ubuntu:22.04") we would incorrectly return "docker.io" and
	// attempt to rewrite public images. Guard against this by comparing the
	// normalised host against the raw image string: if the raw string does not
	// start with the extracted host, the host was implicit and should be
	// treated as absent.
	if !strings.HasPrefix(image, host) {
		return ""
	}
	return host
}

// replaceRegistryHost replaces oldHost with newHost at the start of image.
// It performs a single prefix replacement and preserves the rest of the
// reference (repository path, tag, digest) verbatim.
func replaceRegistryHost(image, oldHost, newHost string) string {
	if !strings.HasPrefix(image, oldHost) {
		return image
	}
	return newHost + image[len(oldHost):]
}
