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

package syncmgr

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// ── API Key handlers ──────────────────────────────────────────────────────────

type internalCreateRequest struct {
	Namespace   string `json:"namespace"`
	User        string `json:"user"`
	Team        string `json:"team"`
	Role        string `json:"role"`
	Description string `json:"description"`
	QuotaURL    string `json:"quotaURL"`
	ExpiresAt   string `json:"expiresAt"`
	// Import-mode fields: when both are set, use CreateFromHash.
	TokenHash  string `json:"tokenHash,omitempty"`
	HashPrefix string `json:"hashPrefix,omitempty"`
	IssuedAt   string `json:"issuedAt,omitempty"`
}

type internalCreateResponse struct {
	RawToken   string `json:"rawToken"`
	KeyID      string `json:"keyId"`
	HashPrefix string `json:"hashPrefix"`
	IssuedAt   string `json:"issuedAt"`
	Namespace  string `json:"namespace"`
	User       string `json:"user"`
	Team       string `json:"team"`
}

func (m *SyncManager) handleInternalCreate(c *gin.Context) {
	var req internalCreateRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	if req.TokenHash != "" && req.HashPrefix != "" {
		m.handleInternalImport(c, req)
		return
	}

	if m.deps.KeyStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key store not configured"})
		return
	}

	ctx := c.Request.Context()

	if m.deps.MaxPerUser > 0 {
		count, err := m.deps.KeyStore.CountUserKeys(ctx, req.Namespace, req.User)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if count >= m.deps.MaxPerUser {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("exceeded max keys per user (%d)", m.deps.MaxPerUser)})
			return
		}
	}

	var expiresAt time.Time
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			expiresAt = t
		}
	}

	role := req.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        role,
		Description: req.Description,
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   expiresAt,
	}

	rawToken, keyID, err := m.deps.KeyStore.Create(ctx, meta)
	if err != nil {
		log.Printf("syncManager: internal create error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create key"})
		return
	}

	hashPrefix := ""
	const pfx = "agentbox-apikey-"
	if strings.HasPrefix(keyID, pfx) {
		hashPrefix = keyID[len(pfx):]
	}

	createdMeta, _ := m.deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		m.broadcastKeyUpsert(metaToProto(*createdMeta))
	}

	c.JSON(http.StatusCreated, internalCreateResponse{
		RawToken:   rawToken,
		KeyID:      keyID,
		HashPrefix: hashPrefix,
		IssuedAt:   meta.IssuedAt.UTC().Format(time.RFC3339),
		Namespace:  req.Namespace,
		User:       req.User,
		Team:       req.Team,
	})
}

func (m *SyncManager) handleInternalImport(c *gin.Context, req internalCreateRequest) {
	if m.deps.KeyStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key store not configured"})
		return
	}

	ctx := c.Request.Context()

	issuedAt := time.Now().UTC()
	if req.IssuedAt != "" {
		if t, err := time.Parse(time.RFC3339, req.IssuedAt); err == nil {
			issuedAt = t
		}
	}

	var expiresAt time.Time
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			expiresAt = t
		}
	}

	role := req.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        role,
		Description: req.Description,
		QuotaURL:    req.QuotaURL,
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
	}

	if err := m.deps.KeyStore.CreateFromHash(ctx, meta, req.TokenHash, req.HashPrefix); err != nil {
		log.Printf("syncManager: internal import error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to import key"})
		return
	}

	keyID := "agentbox-apikey-" + req.HashPrefix

	createdMeta, _ := m.deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		m.broadcastKeyUpsert(metaToProto(*createdMeta))
	}

	c.JSON(http.StatusCreated, internalCreateResponse{
		RawToken:   "",
		KeyID:      keyID,
		HashPrefix: req.HashPrefix,
		IssuedAt:   issuedAt.UTC().Format(time.RFC3339),
		Namespace:  req.Namespace,
		User:       req.User,
		Team:       req.Team,
	})
}

func (m *SyncManager) handleInternalDelete(c *gin.Context) {
	if m.deps.KeyStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key store not configured"})
		return
	}
	name := c.Param("name")
	ctx := c.Request.Context()

	if err := m.deps.KeyStore.Delete(ctx, name); err != nil {
		if err == apikey.ErrTokenNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		log.Printf("syncManager: internal delete error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete key"})
		return
	}
	m.broadcastKeyDelete(name)
	c.Status(http.StatusNoContent)
}

func (m *SyncManager) handleInternalList(c *gin.Context) {
	if m.deps.KeyStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key store not configured"})
		return
	}
	ctx := c.Request.Context()
	team := c.Query("team")
	user := c.Query("user")

	var metas []apikey.KeyMetadata
	var err error
	if team != "" || user != "" {
		metas, err = m.deps.KeyStore.ListByTeamAndUser(ctx, team, user)
	} else {
		metas, err = m.deps.KeyStore.List(ctx)
	}
	if err != nil {
		log.Printf("syncManager: internal list error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list keys"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": metas})
}

// ── Cluster status / heartbeat endpoints ─────────────────────────────────────

type clusterStatusEntry struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Connected      bool    `json:"connected"`
	ConnectedAt    *string `json:"connectedAt"`
	DisconnectedAt *string `json:"disconnectedAt"`
	LastFrameAt    *string `json:"lastFrameAt"`
}

func (m *SyncManager) handleClusterStatus(c *gin.Context) {
	all := m.clusters.All()

	type connSnapshot struct {
		conn             *clusterSyncConn
		lastDisconnected time.Time
	}
	m.mu.RLock()
	snaps := make(map[string]connSnapshot, len(m.registry))
	for id, conn := range m.registry {
		snaps[id] = connSnapshot{conn: conn}
	}
	for id, t := range m.lastDisconnected {
		s := snaps[id]
		s.lastDisconnected = t
		snaps[id] = s
	}
	m.mu.RUnlock()

	entries := make([]clusterStatusEntry, 0, len(all))
	for _, cl := range all {
		entry := clusterStatusEntry{
			ID:   cl.ID,
			Name: cl.Name,
		}
		snap, connected := snaps[cl.ID]
		entry.Connected = connected

		if connected && snap.conn != nil {
			s := snap.conn.connectedAt.UTC().Format(time.RFC3339)
			entry.ConnectedAt = &s

			if lf := snap.conn.lastFrameAt(); !lf.IsZero() {
				lfStr := lf.UTC().Format(time.RFC3339)
				entry.LastFrameAt = &lfStr
			}
		}

		if !snap.lastDisconnected.IsZero() {
			s := snap.lastDisconnected.UTC().Format(time.RFC3339)
			entry.DisconnectedAt = &s
		}

		entries = append(entries, entry)
	}

	c.JSON(http.StatusOK, gin.H{"clusters": entries})
}

func (m *SyncManager) handleInternalStatus(c *gin.Context) {
	total := len(m.clusters.All())

	m.mu.RLock()
	connected := len(m.registry)
	m.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"totalClusters":     total,
		"connectedClusters": connected,
		"uptime":            time.Since(m.startedAt).Round(time.Second).String(),
		"startedAt":         m.startedAt.UTC().Format(time.RFC3339),
	})
}
