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

package config_test

import (
	"flag"
	"os"
	"testing"

	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
)

func TestConfig_Defaults(t *testing.T) {
	// Ensure none of the relevant env vars are set.
	for _, key := range []string{
		"WSPROXY_LISTEN_ADDR", "WSPROXY_INTERNAL_ADDR", "CLUSTERS_CONFIG_PATH",
		"AGENTBOX_SYNC_TOKEN", "AGENTBOX_MANAGER_TOKEN", "AGENTBOX_ADMIN_KEY",
		"JWT_SECRET", "AGENTBOX_MAX_KEYS_PER_USER", "AGENTBOX_APIKEY_NAMESPACE",
	} {
		t.Setenv(key, "")
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.FromFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.ListenAddr != ":9003" {
		t.Errorf("ListenAddr = %q, want :9003", cfg.ListenAddr)
	}
	if cfg.InternalAddr != ":9004" {
		t.Errorf("InternalAddr = %q, want :9004", cfg.InternalAddr)
	}
	if cfg.ClustersFilePath != "/etc/agentbox/clusters.yaml" {
		t.Errorf("ClustersFilePath = %q", cfg.ClustersFilePath)
	}
	if cfg.APIKeyNamespace != "agentbox-system" {
		t.Errorf("APIKeyNamespace = %q, want agentbox-system", cfg.APIKeyNamespace)
	}
	if cfg.MaxKeysPerUser != 0 {
		t.Errorf("MaxKeysPerUser = %d, want 0", cfg.MaxKeysPerUser)
	}
	if cfg.SyncEnabled() {
		t.Errorf("SyncEnabled should be false when SyncToken is empty")
	}
}

func TestConfig_EnvOverride(t *testing.T) {
	_ = os.Setenv("WSPROXY_LISTEN_ADDR", ":9999")
	_ = os.Setenv("AGENTBOX_SYNC_TOKEN", "mysecret")
	_ = os.Setenv("AGENTBOX_MAX_KEYS_PER_USER", "5")
	_ = os.Setenv("AGENTBOX_APIKEY_NAMESPACE", "custom-ns")
	t.Cleanup(func() {
		_ = os.Unsetenv("WSPROXY_LISTEN_ADDR")
		_ = os.Unsetenv("AGENTBOX_SYNC_TOKEN")
		_ = os.Unsetenv("AGENTBOX_MAX_KEYS_PER_USER")
		_ = os.Unsetenv("AGENTBOX_APIKEY_NAMESPACE")
	})

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.FromFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999", cfg.ListenAddr)
	}
	if cfg.SyncToken != "mysecret" {
		t.Errorf("SyncToken = %q, want mysecret", cfg.SyncToken)
	}
	if cfg.MaxKeysPerUser != 5 {
		t.Errorf("MaxKeysPerUser = %d, want 5", cfg.MaxKeysPerUser)
	}
	if cfg.APIKeyNamespace != "custom-ns" {
		t.Errorf("APIKeyNamespace = %q, want custom-ns", cfg.APIKeyNamespace)
	}
	if !cfg.SyncEnabled() {
		t.Errorf("SyncEnabled should be true when SyncToken is non-empty")
	}
}

func TestConfig_Validate_NoSyncToken(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.FromFlags(fs)
	_ = fs.Parse(nil)

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with empty SyncToken should return nil, got %v", err)
	}
}

func TestConfig_FlagOverridesEnv(t *testing.T) {
	_ = os.Setenv("WSPROXY_LISTEN_ADDR", ":8888")
	t.Cleanup(func() { _ = os.Unsetenv("WSPROXY_LISTEN_ADDR") })

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.FromFlags(fs)
	if err := fs.Parse([]string{"--listen-addr=:7777"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Explicit flag value takes precedence over env var.
	if cfg.ListenAddr != ":7777" {
		t.Errorf("ListenAddr = %q, want :7777 (flag should override env)", cfg.ListenAddr)
	}
}
