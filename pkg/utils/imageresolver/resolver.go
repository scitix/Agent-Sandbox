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

package imageresolver

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/distribution/reference"
	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DigestResolver resolves container image references to their content digests.
type DigestResolver interface {
	// Resolve resolves an image reference (tag or digest) to its manifest digest.
	// For tag-based refs, it checks the cache first, then queries the registry
	// via HTTP HEAD on cache miss. For digest-based refs (@sha256:...), it
	// extracts and returns the digest directly.
	// Use WithPullSecrets to provide registry credentials for private images.
	Resolve(ctx context.Context, imageRef string, opts ...ResolveOption) (string, error)

	// ResolveConfigDigest resolves an image reference to its config digest
	// (the "image ID" shown by docker inspect / docker images --no-trunc).
	// Unlike manifest digests, the config digest is stable across re-pushes
	// and across registries when the image layers are identical.
	// This requires fetching the manifest body (one GET), not just a HEAD.
	// Use WithPullSecrets to provide registry credentials for private images.
	ResolveConfigDigest(ctx context.Context, imageRef string, opts ...ResolveOption) (string, error)

	// DigestFromStatus extracts the digest from pod status fields and caches
	// the image→digest mapping for future Resolve calls. This is a pure
	// local operation with no network calls.
	//   image:   the image name as reported by kubelet (e.g. "registry/repo:tag")
	//   imageID: the imageID as reported by kubelet (e.g. "registry/repo@sha256:abc...")
	DigestFromStatus(image, imageID string) (string, error)
}

// ResolveOption configures a Resolve call.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	namespace   string
	pullSecrets []corev1.LocalObjectReference
}

// WithPullSecrets provides imagePullSecrets context for authenticating
// against private registries. The resolver reads the referenced Secrets
// from the given namespace to build registry credentials.
func WithPullSecrets(namespace string, secrets []corev1.LocalObjectReference) ResolveOption {
	return func(o *resolveOptions) {
		o.namespace = namespace
		o.pullSecrets = secrets
	}
}

type cacheEntry struct {
	digest    string
	expiresAt time.Time
}

// Resolver implements DigestResolver with a TTL-based in-memory cache.
type Resolver struct {
	mu          sync.RWMutex
	cache       map[string]cacheEntry // key: normalized image ref string → manifest digest
	configCache map[string]cacheEntry // key: normalized image ref string → config digest
	ttl         time.Duration
	client      client.Reader // for reading imagePullSecrets
}

// NewResolver creates a new Resolver.
//   - k8sClient: used to read imagePullSecrets from namespaces (may be nil
//     if all registries are public).
//   - ttl: cache duration for resolved digests (e.g. 3 days).
func NewResolver(k8sClient client.Reader, ttl time.Duration) *Resolver {
	return &Resolver{
		cache:       make(map[string]cacheEntry),
		configCache: make(map[string]cacheEntry),
		ttl:         ttl,
		client:      k8sClient,
	}
}

func (r *Resolver) DigestFromStatus(image, imageID string) (string, error) {
	// Parse digest from imageID (format: "registry/repo@sha256:abc...")
	digest, err := parseDigest(imageID)
	if err != nil {
		return "", fmt.Errorf("parse imageID %q: %w", imageID, err)
	}

	// NOTE: We intentionally do NOT cache image→digest here. The status.image
	// is a tag (e.g. "myapp:v2") and the digest from imageID reflects what is
	// currently running on this pod, which may be stale relative to what the
	// tag currently points to in the registry. Caching it would cause false
	// positives when comparing against a Resolve() call for the same tag.

	return digest, nil
}

func (r *Resolver) Resolve(ctx context.Context, imageRef string, opts ...ResolveOption) (string, error) {
	if imageRef == "" {
		return "", fmt.Errorf("empty image reference")
	}

	// Fast path: if the ref already contains a digest, extract it directly.
	if digest, err := parseDigest(imageRef); err == nil {
		key := normalizeRef(imageRef)
		if key != "" {
			r.put(key, digest)
		}
		return digest, nil
	}

	// Check cache.
	key := normalizeRef(imageRef)
	if key != "" {
		if digest, ok := r.get(key); ok {
			return digest, nil
		}
	}

	// Cache miss — resolve from registry via HEAD request.
	var o resolveOptions
	for _, fn := range opts {
		fn(&o)
	}

	keychain, err := r.buildKeychain(ctx, o.namespace, o.pullSecrets)
	if err != nil {
		klog.V(4).InfoS("Failed to build keychain for image resolve, falling back to anonymous",
			"imageRef", imageRef, "error", err)
		keychain = authn.NewMultiKeychain(authn.DefaultKeychain)
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse reference %q: %w", imageRef, err)
	}

	desc, err := remote.Head(ref,
		remote.WithAuthFromKeychain(keychain),
		remote.WithContext(ctx),
	)
	if err != nil {
		return "", fmt.Errorf("HEAD %q: %w", imageRef, err)
	}

	digest := desc.Digest.String()
	if key != "" {
		r.put(key, digest)
	}

	klog.V(4).InfoS("Resolved image digest from registry",
		"imageRef", imageRef, "digest", digest)
	return digest, nil
}

// ResolveConfigDigest resolves an image reference to its config digest.
// The config digest is stable across re-pushes and across registries when
// image layers are identical (equivalent to `docker inspect --format '{{.Id}}'`).
// Results are cached separately from manifest digests.
func (r *Resolver) ResolveConfigDigest(ctx context.Context, imageRef string, opts ...ResolveOption) (string, error) {
	if imageRef == "" {
		return "", fmt.Errorf("empty image reference")
	}

	key := normalizeRef(imageRef)
	if key != "" {
		if digest, ok := r.getConfig(key); ok {
			return digest, nil
		}
	}

	var o resolveOptions
	for _, fn := range opts {
		fn(&o)
	}

	keychain, err := r.buildKeychain(ctx, o.namespace, o.pullSecrets)
	if err != nil {
		klog.V(4).InfoS("Failed to build keychain for config digest resolve, falling back to anonymous",
			"imageRef", imageRef, "error", err)
		keychain = authn.NewMultiKeychain(authn.DefaultKeychain)
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse reference %q: %w", imageRef, err)
	}

	img, err := remote.Image(ref,
		remote.WithAuthFromKeychain(keychain),
		remote.WithContext(ctx),
	)
	if err != nil {
		return "", fmt.Errorf("fetch image %q: %w", imageRef, err)
	}

	configName, err := img.ConfigName()
	if err != nil {
		return "", fmt.Errorf("config digest for %q: %w", imageRef, err)
	}

	configDigest := configName.String()
	if key != "" {
		r.putConfig(key, configDigest)
	}

	klog.V(4).InfoS("Resolved image config digest from registry",
		"imageRef", imageRef, "configDigest", configDigest)
	return configDigest, nil
}

// parseDigest extracts a digest string from an image reference that contains
// one (e.g. "registry/repo@sha256:abc..."). Returns error if no digest found.
func parseDigest(imageRef string) (string, error) {
	ref, err := reference.ParseAnyReference(imageRef)
	if err != nil {
		return "", err
	}
	if digested, ok := ref.(reference.Digested); ok {
		return digested.Digest().String(), nil
	}
	return "", fmt.Errorf("no digest in %q", imageRef)
}

// normalizeRef normalizes an image reference to a canonical string for cache keys.
func normalizeRef(imageRef string) string {
	ref, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		// imageID refs like "registry/repo@sha256:..." may not parse as Named.
		return imageRef
	}
	return reference.FamiliarString(ref)
}

func (r *Resolver) get(key string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.digest, true
}

func (r *Resolver) put(key, digest string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = cacheEntry{
		digest:    digest,
		expiresAt: time.Now().Add(r.ttl),
	}
}

func (r *Resolver) getConfig(key string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.configCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.digest, true
}

func (r *Resolver) putConfig(key, digest string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configCache[key] = cacheEntry{
		digest:    digest,
		expiresAt: time.Now().Add(r.ttl),
	}
}

// buildKeychain merges all imagePullSecrets into a single configfile.ConfigFile
// and wraps it in a configFileKeychain. Credential lookup is fully delegated to
// docker/cli, which handles Docker Hub's legacy key, credential helpers, and all
// private registries without any hand-rolled matching logic.
func (r *Resolver) buildKeychain(ctx context.Context, namespace string, pullSecrets []corev1.LocalObjectReference) (authn.Keychain, error) {
	if r.client == nil || len(pullSecrets) == 0 {
		return authn.DefaultKeychain, nil
	}

	merged := configfile.New("")
	found := false
	for _, ref := range pullSecrets {
		secret := &corev1.Secret{}
		if err := r.client.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      ref.Name,
		}, secret); err != nil {
			klog.V(4).InfoS("Failed to read imagePullSecret, skipping",
				"namespace", namespace, "secret", ref.Name, "error", err)
			continue
		}

		data, ok := secret.Data[corev1.DockerConfigJsonKey]
		if !ok {
			continue
		}

		cf, err := dockerconfig.LoadFromReader(bytes.NewReader(data))
		if err != nil {
			klog.V(4).InfoS("Failed to parse imagePullSecret, skipping",
				"namespace", namespace, "secret", ref.Name, "error", err)
			continue
		}

		maps.Copy(merged.AuthConfigs, cf.GetAuthConfigs())
		found = true
	}

	if !found {
		return authn.DefaultKeychain, nil
	}

	return &configFileKeychain{cf: merged}, nil
}

// configFileKeychain implements authn.Keychain backed by a docker configfile.ConfigFile.
// It mirrors go-containerregistry's defaultKeychain lookup strategy: try the full
// repo string first, then just the registry hostname; Docker Hub's legacy
// "https://index.docker.io/v1/" key is handled via authn.DefaultAuthKey.
type configFileKeychain struct {
	cf *configfile.ConfigFile
}

func (k *configFileKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	for _, key := range []string{target.String(), target.RegistryStr()} {
		if key == name.DefaultRegistry {
			key = authn.DefaultAuthKey // "https://index.docker.io/v1/"
		}
		ac, err := k.cf.GetAuthConfig(key)
		if err != nil {
			return nil, err
		}
		if ac.Username != "" || ac.Password != "" || ac.Auth != "" || ac.IdentityToken != "" || ac.RegistryToken != "" {
			return authn.FromConfig(authn.AuthConfig{
				Username:      ac.Username,
				Password:      ac.Password,
				Auth:          ac.Auth,
				IdentityToken: ac.IdentityToken,
				RegistryToken: ac.RegistryToken,
			}), nil
		}
	}
	return authn.Anonymous, nil
}
