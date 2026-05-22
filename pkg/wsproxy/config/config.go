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

// Package config holds the wsproxy runtime configuration.
// All env-var defaults and flag registrations are centralised here so the
// rest of the codebase is free of os.Getenv calls.
package config

import (
	"flag"
	"os"
	"strconv"
)

const (
	defaultListenAddr             = ":9003"
	defaultInternalAddr           = ":9004"
	defaultClustersFilePath       = "/etc/agentbox/clusters.yaml"
	defaultAPIKeyNamespace        = "agentbox-system"
	defaultImagesCatalogConfigMap = "agentbox-images-catalog"
)

// Config holds all wsproxy runtime settings.
// Each field maps 1:1 to a CLI flag whose default value is the matching
// environment variable (or a hard-coded default when the env var is absent).
type Config struct {
	// ListenAddr is the address the terminal WebSocket proxy listens on.
	// Flag: --listen-addr  Env: WSPROXY_LISTEN_ADDR  Default: :9003
	ListenAddr string

	// InternalAddr is the address the internal management API listens on.
	// Only used when SyncToken is non-empty.
	// Flag: --internal-addr  Env: WSPROXY_INTERNAL_ADDR  Default: :9004
	InternalAddr string

	// ClustersFilePath is the path to clusters.yaml consumed by cluster.Store.
	// Flag: --clusters-config  Env: CLUSTERS_CONFIG_PATH
	ClustersFilePath string

	// Secret is the single shared secret used for:
	//   - dialling Worker /v1/ws/sync endpoints (AGENTBOX-SYNC-TOKEN header);
	//   - gating the legacy /internal/* routes (static token check);
	//   - verifying Bearer JWTs issued by the BFF (HS256).
	// When empty the sync manager is disabled and JWT auth is off.
	// Flag: --secret  Env: AGENTBOX_SECRET
	Secret string

	// AdminKey is the shared admin API key used by the internal API auth
	// middleware to recognise admin callers via AGENTBOX-API-KEY header.
	// When empty the internal API runs in dev mode (anonymous admin).
	// Flag: --admin-key  Env: AGENTBOX_ADMIN_KEY
	AdminKey string

	// MaxKeysPerUser is the per-(namespace, user) API key count limit.
	// 0 means unlimited.
	// Flag: --max-keys-per-user  Env: AGENTBOX_MAX_KEYS_PER_USER
	MaxKeysPerUser int

	// APIKeyNamespace is the Kubernetes namespace where API key Secrets are stored.
	// Flag: --apikey-namespace  Env: AGENTBOX_APIKEY_NAMESPACE  Default: agentbox-system
	APIKeyNamespace string

	// ImagesCatalogConfigMap is the name of the ConfigMap that holds the images
	// catalog. It is stored in APIKeyNamespace.
	// Flag: --images-catalog-configmap  Env: AGENTBOX_IMAGES_CATALOG_CONFIGMAP
	// Default: agentbox-images-catalog
	ImagesCatalogConfigMap string
}

// FromFlags registers all wsproxy flags on fs and returns a *Config whose
// fields point to the registered flag values. Call flag.Parse() (or
// fs.Parse()) after this to populate the fields.
//
// Each flag's default is the corresponding environment variable, falling back
// to the built-in default when the env var is absent.
func FromFlags(fs *flag.FlagSet) *Config {
	cfg := &Config{}

	fs.StringVar(&cfg.ListenAddr, "listen-addr",
		envOr("WSPROXY_LISTEN_ADDR", defaultListenAddr),
		"Address the terminal WebSocket proxy listens on.")

	fs.StringVar(&cfg.InternalAddr, "internal-addr",
		envOr("WSPROXY_INTERNAL_ADDR", defaultInternalAddr),
		"Address the internal management API listens on (requires --sync-token).")

	fs.StringVar(&cfg.ClustersFilePath, "clusters-config",
		envOr("CLUSTERS_CONFIG_PATH", defaultClustersFilePath),
		"Path to the clusters.yaml file.")

	fs.StringVar(&cfg.Secret, "secret",
		os.Getenv("AGENTBOX_SECRET"),
		"Shared secret for /v1/ws/sync dialling, /internal/* token gate, and BFF JWT (HS256) verification. "+
			"Empty = sync manager and JWT auth disabled.")

	fs.StringVar(&cfg.AdminKey, "admin-key",
		os.Getenv("AGENTBOX_ADMIN_KEY"),
		"Admin API key for internal API auth. Empty = dev mode (anonymous admin).")

	fs.IntVar(&cfg.MaxKeysPerUser, "max-keys-per-user",
		envInt("AGENTBOX_MAX_KEYS_PER_USER", 0),
		"Per-user API key count limit (0 = unlimited).")

	fs.StringVar(&cfg.APIKeyNamespace, "apikey-namespace",
		envOr("AGENTBOX_APIKEY_NAMESPACE", defaultAPIKeyNamespace),
		"Kubernetes namespace for API key Secrets.")

	fs.StringVar(&cfg.ImagesCatalogConfigMap, "images-catalog-configmap",
		envOr("AGENTBOX_IMAGES_CATALOG_CONFIGMAP", defaultImagesCatalogConfigMap),
		"Name of the ConfigMap holding the images catalog (stored in --apikey-namespace).")

	return cfg
}

// Validate checks that the Config is consistent. It does not require
// Secret to be set — an empty Secret simply disables the sync manager
// and internal API.
func (c *Config) Validate() error {
	return nil
}

// SyncEnabled reports whether the sync manager (and internal API) should be
// started. It is true when Secret is non-empty.
func (c *Config) SyncEnabled() bool {
	return c.Secret != ""
}

// ── helpers ───────────────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
