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

package apikey

import (
	"crypto/subtle"
	"strings"
)

const (
	// RoleAdmin is the admin role string.
	RoleAdmin = "admin"
	// RoleTenant is the tenant role string.
	RoleTenant = "tenant"
)

// AdminKeyManager handles constant-time comparison of the admin API key.
type AdminKeyManager struct {
	adminKey string
}

// NewAdminKeyManager creates a new AdminKeyManager.
// adminKey is the raw admin key string. When empty, IsAdminKey always returns false.
func NewAdminKeyManager(adminKey string) *AdminKeyManager {
	return &AdminKeyManager{adminKey: adminKey}
}

// IsAdminKey reports whether the provided key equals the admin key using
// constant-time comparison to prevent timing attacks.
func (m *AdminKeyManager) IsAdminKey(key string) bool {
	if m.adminKey == "" {
		return false
	}
	return subtle.ConstantTimeCompare(
		[]byte(strings.TrimSpace(key)),
		[]byte(m.adminKey),
	) == 1
}
