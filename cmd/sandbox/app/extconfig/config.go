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

// Package extconfig defines the schema and loader for the extension
// configuration file (--extension-config). The file lets out-of-tree callers
// supply plugin-specific parameters without polluting the core binary's flag set.
//
// Schema (YAML):
//
//	providers:
//	  quota:
//	    name: bob
//	    args:
//	      namespace: bob-namespace
//	  instanceType:
//	    name: dave
//	    args:
//	      configMapNamespace: alice-namespace
//	      configMapName: alice-config
//
//	plugins:
//	  - name: carol
//	    args:
//	      schedulerURL: http://...
package extconfig

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// ExtensionConfig is the top-level structure of the extension config file.
type ExtensionConfig struct {
	Providers *ProvidersConfig `json:"providers,omitempty"`
	Plugins   []PluginConfig   `json:"plugins,omitempty"`
}

// ProvidersConfig groups all data-source provider configurations.
type ProvidersConfig struct {
	Quota        *ProviderConfig `json:"quota,omitempty"`
	InstanceType *ProviderConfig `json:"instanceType,omitempty"`
}

// ProviderConfig holds the name and raw args for a provider (quota,
// instancetype, …).
type ProviderConfig struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// PluginConfig holds the name and raw args for a single lifecycle plugin.
type PluginConfig struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// Load reads a YAML extension config file from path and returns the decoded
// ExtensionConfig. Returns a zero-value config (no provider, no plugins) when
// path is empty, so callers can always treat the result as valid.
func Load(path string) (*ExtensionConfig, error) {
	if path == "" {
		return &ExtensionConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("extconfig: read %q: %w", path, err)
	}
	cfg := &ExtensionConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("extconfig: parse %q: %w", path, err)
	}
	return cfg, nil
}

// Plugin looks up a PluginConfig by name. Returns (nil, false) if not found.
func (c *ExtensionConfig) Plugin(name string) (*PluginConfig, bool) {
	for i := range c.Plugins {
		if c.Plugins[i].Name == name {
			return &c.Plugins[i], true
		}
	}
	return nil, false
}
