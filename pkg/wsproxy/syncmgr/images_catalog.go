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

// Images Catalog CRUD — stored as a single ConfigMap in the master cluster.
//
// Ownership: the catalog is runtime-mutable state written here. When the Helm
// chart is deployed with imagesCatalog.manageConfigMap=false (e.g. installers
// that render static YAML and `kubectl apply` it, where Helm's `lookup` cannot
// preserve live data), the chart does NOT ship the ConfigMap — ws-proxy is its
// sole owner: ensureImagesCatalog bootstraps an empty object on startup and the
// CRUD handlers below are the only writers, so re-applying the installer never
// clobbers runtime data.
//
// Endpoints (all under the /internal prefix, manager-token auth):
//   GET    /internal/images-catalog          → list all datasets
//   POST   /internal/images-catalog          → upsert a single dataset
//   PUT    /internal/images-catalog/:id      → upsert one dataset
//   DELETE /internal/images-catalog/:id      → remove one dataset

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const imagesCatalogKey = "images-catalog.json"

// ImageDataset mirrors the TypeScript type in components/images/data.ts.
type ImageDataset struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	ImageCount     int               `json:"imageCount"`
	Category       string            `json:"category"`
	Source         string            `json:"source"`
	HuggingFaceURL string            `json:"huggingFaceUrl"`
	Tags           []string          `json:"tags"`
	ClusterDocs    map[string]string `json:"clusterDocs"`
}

// ensureImagesCatalog bootstraps the images-catalog ConfigMap if it does not
// yet exist, seeding it with an empty dataset list. It NEVER overwrites an
// existing ConfigMap, so runtime data written via the CRUD handlers survives
// ws-proxy restarts and (when the chart does not manage the object) installer
// re-applies. Idempotent and best-effort: errors are logged, not fatal.
func (m *SyncManager) ensureImagesCatalog(ctx context.Context) {
	if m.deps.TemplateClient == nil {
		return
	}
	cm := &corev1.ConfigMap{}
	err := m.deps.TemplateClient.Get(ctx, client.ObjectKey{
		Namespace: m.deps.ImagesCatalogNamespace,
		Name:      m.deps.ImagesCatalogConfigMap,
	}, cm)
	if err == nil {
		return // already exists; leave runtime data intact
	}
	if !apierrors.IsNotFound(err) {
		log.Printf("wsproxy: images catalog ensure get error: %v", err)
		return
	}
	newCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.deps.ImagesCatalogConfigMap,
			Namespace: m.deps.ImagesCatalogNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentbox-dashboard",
			},
		},
		Data: map[string]string{imagesCatalogKey: "[]"},
	}
	if err := m.deps.TemplateClient.Create(ctx, newCM); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Printf("wsproxy: images catalog ensure create error: %v", err)
		return
	}
	log.Printf("wsproxy: bootstrapped images catalog ConfigMap %s/%s",
		m.deps.ImagesCatalogNamespace, m.deps.ImagesCatalogConfigMap)
}

func (m *SyncManager) loadCatalog(ctx context.Context) ([]ImageDataset, error) {
	if m.deps.TemplateClient == nil {
		return nil, nil
	}
	cm := &corev1.ConfigMap{}
	err := m.deps.TemplateClient.Get(ctx, client.ObjectKey{
		Namespace: m.deps.ImagesCatalogNamespace,
		Name:      m.deps.ImagesCatalogConfigMap,
	}, cm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return []ImageDataset{}, nil
		}
		return nil, err
	}
	raw, ok := cm.Data[imagesCatalogKey]
	if !ok || raw == "" {
		return []ImageDataset{}, nil
	}
	var datasets []ImageDataset
	if err := json.Unmarshal([]byte(raw), &datasets); err != nil {
		return nil, err
	}
	return datasets, nil
}

func (m *SyncManager) saveCatalog(ctx context.Context, datasets []ImageDataset) error {
	if m.deps.TemplateClient == nil {
		return nil
	}
	raw, err := json.Marshal(datasets)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{}
	err = m.deps.TemplateClient.Get(ctx, client.ObjectKey{
		Namespace: m.deps.ImagesCatalogNamespace,
		Name:      m.deps.ImagesCatalogConfigMap,
	}, cm)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      m.deps.ImagesCatalogConfigMap,
				Namespace: m.deps.ImagesCatalogNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "agentbox-dashboard",
				},
			},
			Data: map[string]string{
				imagesCatalogKey: string(raw),
			},
		}
		return m.deps.TemplateClient.Create(ctx, newCM)
	}

	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[imagesCatalogKey] = string(raw)
	return m.deps.TemplateClient.Update(ctx, updated)
}

func (m *SyncManager) handleImagesCatalogList(c *gin.Context) {
	if m.deps.TemplateClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "k8s client not configured"})
		return
	}
	datasets, err := m.loadCatalog(c.Request.Context())
	if err != nil {
		log.Printf("wsproxy: images catalog list error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load catalog"})
		return
	}
	c.JSON(http.StatusOK, datasets)
}

func (m *SyncManager) handleImagesCatalogUpsert(c *gin.Context) {
	if m.deps.TemplateClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "k8s client not configured"})
		return
	}
	var dataset ImageDataset
	if err := json.NewDecoder(c.Request.Body).Decode(&dataset); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if dataset.ID == "" || dataset.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id and name are required"})
		return
	}

	ctx := c.Request.Context()
	datasets, err := m.loadCatalog(ctx)
	if err != nil {
		log.Printf("wsproxy: images catalog upsert load error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load catalog"})
		return
	}

	found := false
	for i, d := range datasets {
		if d.ID == dataset.ID {
			datasets[i] = dataset
			found = true
			break
		}
	}
	if !found {
		datasets = append(datasets, dataset)
	}

	if err := m.saveCatalog(ctx, datasets); err != nil {
		log.Printf("wsproxy: images catalog upsert save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save catalog"})
		return
	}

	status := http.StatusOK
	if !found {
		status = http.StatusCreated
	}
	c.JSON(status, dataset)
}

func (m *SyncManager) handleImagesCatalogDelete(c *gin.Context) {
	if m.deps.TemplateClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "k8s client not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx := c.Request.Context()
	datasets, err := m.loadCatalog(ctx)
	if err != nil {
		log.Printf("wsproxy: images catalog delete load error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load catalog"})
		return
	}

	filtered := datasets[:0]
	found := false
	for _, d := range datasets {
		if d.ID == id {
			found = true
		} else {
			filtered = append(filtered, d)
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "dataset not found"})
		return
	}

	if err := m.saveCatalog(ctx, filtered); err != nil {
		log.Printf("wsproxy: images catalog delete save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save catalog"})
		return
	}

	c.Status(http.StatusNoContent)
}
