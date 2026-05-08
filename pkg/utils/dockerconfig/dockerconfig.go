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

// Package dockerconfig builds and parses Kubernetes `.dockerconfigjson`
// payloads for imagePullSecret Secrets of type kubernetes.io/dockerconfigjson.
package dockerconfig

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// RegistryCredential is a single registry auth entry.
type RegistryCredential struct {
	Registry string
	Username string
	Password string
}

// DockerConfig is the top-level shape of .dockerconfigjson.
type DockerConfig struct {
	Auths map[string]DockerAuthEntry `json:"auths"`
}

// DockerAuthEntry is one entry under auths[registry].
type DockerAuthEntry struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

// Build encodes the given credentials into a canonical `.dockerconfigjson` byte slice.
// It computes the `auth` field (base64("user:pass")) automatically and rejects empty input.
func Build(creds []RegistryCredential) ([]byte, error) {
	if len(creds) == 0 {
		return nil, fmt.Errorf("at least one registry credential is required")
	}
	auths := make(map[string]DockerAuthEntry, len(creds))
	for i, c := range creds {
		registry := strings.TrimSpace(c.Registry)
		username := c.Username
		password := c.Password
		if registry == "" {
			return nil, fmt.Errorf("registries[%d].registry is required", i)
		}
		if username == "" {
			return nil, fmt.Errorf("registries[%d].username is required", i)
		}
		if password == "" {
			return nil, fmt.Errorf("registries[%d].password is required", i)
		}
		if _, exists := auths[registry]; exists {
			return nil, fmt.Errorf("duplicate registry %q in credentials", registry)
		}
		auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		auths[registry] = DockerAuthEntry{
			Username: username,
			Password: password,
			Auth:     auth,
		}
	}
	cfg := DockerConfig{Auths: auths}
	return json.Marshal(cfg)
}

// Parse decodes a `.dockerconfigjson` payload back into a flat list of credentials.
// The `auth` field is trusted only when username/password are missing; otherwise the
// explicit username/password are preferred.
func Parse(data []byte) ([]RegistryCredential, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty .dockerconfigjson payload")
	}
	var cfg DockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse .dockerconfigjson: %w", err)
	}
	out := make([]RegistryCredential, 0, len(cfg.Auths))
	for registry, entry := range cfg.Auths {
		username := entry.Username
		password := entry.Password
		if (username == "" || password == "") && entry.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				return nil, fmt.Errorf("decode auth for %q: %w", registry, err)
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				if username == "" {
					username = parts[0]
				}
				if password == "" {
					password = parts[1]
				}
			}
		}
		out = append(out, RegistryCredential{
			Registry: registry,
			Username: username,
			Password: password,
		})
	}
	return out, nil
}
