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
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// managerTokenMiddleware returns a Gin middleware that enforces the AGENTBOX-MANAGER-TOKEN header.
// When token is empty (dev mode), all requests are allowed through.
func managerTokenMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token != "" && c.GetHeader("AGENTBOX-MANAGER-TOKEN") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// InternalAPIHandler returns the HTTP handler for the internal management API (:9004).
func (m *SyncManager) InternalAPIHandler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// /ping is public — no manager token required.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// /metrics is public — Prometheus scrapes without auth.
	r.GET("/metrics", gin.WrapH(MetricsHandler()))

	// All /internal/* routes require the manager token.
	internal := r.Group("/internal", managerTokenMiddleware(m.managerToken))

	// API key endpoints.
	internal.POST("/api-keys", m.handleInternalCreate)
	internal.GET("/api-keys", m.handleInternalList)
	internal.DELETE("/api-keys/:name", m.handleInternalDelete)

	// Broadcast endpoint.
	internal.POST("/broadcast", m.handleInternalBroadcast)

	// SandboxTemplate endpoints.
	internal.GET("/templates", m.handleInternalTemplateList)
	internal.POST("/templates", m.handleInternalTemplateCreate)
	internal.GET("/templates/:name", m.handleInternalTemplateGet)
	internal.PATCH("/templates/:name", m.handleInternalTemplateUpdate)
	internal.PUT("/templates/:name", m.handleInternalTemplateUpdate)
	internal.DELETE("/templates/:name", m.handleInternalTemplateDelete)

	// Cluster heartbeat / status endpoints.
	internal.GET("/clusters/status", m.handleClusterStatus)
	internal.GET("/status", m.handleInternalStatus)

	// Images catalog endpoints.
	internal.GET("/images-catalog", m.handleImagesCatalogList)
	internal.POST("/images-catalog", m.handleImagesCatalogUpsert)
	internal.PUT("/images-catalog/:id", m.handleImagesCatalogUpsert)
	internal.DELETE("/images-catalog/:id", m.handleImagesCatalogDelete)

	return r
}

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
	syncF := protocol.Frame{Type: protocol.FrameKeySync}
	if createdMeta != nil {
		syncF = metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
	}
	m.broadcast(syncF)

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
	syncF := protocol.Frame{Type: protocol.FrameKeySync}
	if createdMeta != nil {
		syncF = metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
	}
	m.broadcast(syncF)

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
	m.broadcast(protocol.Frame{Type: protocol.FrameKeyDeleteSync, Name: name})
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

func (m *SyncManager) handleInternalBroadcast(c *gin.Context) {
	var frame protocol.Frame
	if err := json.NewDecoder(c.Request.Body).Decode(&frame); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	m.broadcast(frame)
	c.Status(http.StatusNoContent)
}

// ── Template handlers ─────────────────────────────────────────────────────────

// appErrStatus maps a domain.AppError to the appropriate HTTP status code.
func appErrStatus(err *domain.AppError) int {
	return int(err.Code)
}

func (m *SyncManager) handleInternalTemplateList(c *gin.Context) {
	if m.deps.TemplateService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template sync not configured"})
		return
	}
	team := c.Query("team")
	user := c.Query("user")
	isAdmin := team == "" && user == ""
	auth := domain.AuthInfo{Team: team, User: user}

	ctx := c.Request.Context()
	items, appErr := m.deps.TemplateService.List(ctx, auth, isAdmin)
	if appErr != nil {
		log.Printf("syncManager: internal template list error: %v", appErr)
		c.JSON(appErrStatus(appErr), gin.H{"error": appErr.Message})
		return
	}

	summaries := make([]any, len(items))
	for i := range items {
		summaries[i] = service.TemplateToSummaryGen(&items[i])
	}
	c.JSON(http.StatusOK, gin.H{"items": summaries, "total": len(summaries)})
}

func (m *SyncManager) handleInternalTemplateGet(c *gin.Context) {
	if m.deps.TemplateService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template sync not configured"})
		return
	}
	name := c.Param("name")
	ctx := c.Request.Context()
	tmpl, appErr := m.deps.TemplateService.Get(ctx, name)
	if appErr != nil {
		c.JSON(appErrStatus(appErr), gin.H{"error": appErr.Message})
		return
	}
	c.JSON(http.StatusOK, service.TemplateToGen(tmpl))
}

func (m *SyncManager) handleInternalTemplateCreate(c *gin.Context) {
	if m.deps.TemplateService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template sync not configured"})
		return
	}

	tmpl, ok := parseCRDBody(c)
	if !ok {
		return
	}
	if tmpl.Labels == nil {
		tmpl.Labels = make(map[string]string)
	}
	tmpl.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal

	ctx := c.Request.Context()
	result, appErr := m.deps.TemplateService.Create(ctx, tmpl)
	if appErr != nil {
		log.Printf("syncManager: internal template create error: %v", appErr)
		c.JSON(appErrStatus(appErr), gin.H{"error": appErr.Message})
		return
	}

	if sf, fErr := templateDomainToFrame(result); fErr == nil {
		sf.Type = protocol.FrameTemplateSync
		m.broadcast(sf)
	}

	c.JSON(http.StatusCreated, gin.H{"name": result.Name})
}

func (m *SyncManager) handleInternalTemplateUpdate(c *gin.Context) {
	if m.deps.TemplateService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template sync not configured"})
		return
	}
	name := c.Param("name")

	desired, ok := parseCRDBody(c)
	if !ok {
		return
	}
	desired.Name = name
	if desired.Labels == nil {
		desired.Labels = make(map[string]string)
	}
	desired.Labels["agentbox.io/sync-source"] = "global"

	ctx := c.Request.Context()
	result, appErr := m.deps.TemplateService.Update(ctx, desired)
	if appErr != nil {
		log.Printf("syncManager: internal template update error: %v", appErr)
		c.JSON(appErrStatus(appErr), gin.H{"error": appErr.Message})
		return
	}

	if sf, fErr := templateDomainToFrame(result); fErr == nil {
		sf.Type = protocol.FrameTemplateSync
		m.broadcast(sf)
	}

	c.JSON(http.StatusOK, gin.H{"name": result.Name})
}

func (m *SyncManager) handleInternalTemplateDelete(c *gin.Context) {
	if m.deps.TemplateService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template sync not configured"})
		return
	}
	name := c.Param("name")
	ctx := c.Request.Context()
	if appErr := m.deps.TemplateService.Delete(ctx, name); appErr != nil {
		log.Printf("syncManager: internal template delete error: %v", appErr)
		c.JSON(appErrStatus(appErr), gin.H{"error": appErr.Message})
		return
	}
	m.broadcast(protocol.Frame{Type: protocol.FrameTemplateDeleteSync, Name: name})
	c.Status(http.StatusNoContent)
}

// parseCRDBody decodes the request body as { "crdObject": <json> } or as a raw
// SandboxTemplate JSON, and unmarshals it into an agentsv1alpha1.SandboxTemplate.
// Returns false and writes an error response if parsing fails.
func parseCRDBody(c *gin.Context) (*agentsv1alpha1.SandboxTemplate, bool) {
	type withCRD struct {
		CRDObject json.RawMessage `json:"crdObject"`
	}
	var wrapper withCRD
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty body"})
		return nil, false
	}
	_ = json.Unmarshal(raw, &wrapper)

	payload := raw
	if len(wrapper.CRDObject) > 0 {
		payload = wrapper.CRDObject
	}

	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := json.Unmarshal(payload, tmpl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return nil, false
	}
	if tmpl.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metadata.name is required"})
		return nil, false
	}
	return tmpl, true
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
