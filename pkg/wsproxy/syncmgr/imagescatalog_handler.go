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
	"context"
	"log"
	"maps"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
)

// ── ListImagesCatalog ─────────────────────────────────────────────────────────

func (s *templateServer) ListImagesCatalog(
	ctx context.Context,
	_ wsproxygen.ListImagesCatalogRequestObject,
) (wsproxygen.ListImagesCatalogResponseObject, error) {
	datasets, err := s.m.loadCatalog(ctx)
	if err != nil {
		log.Printf("syncManager: images catalog list error: %v", err)
		return wsproxygen.ListImagesCatalog503JSONResponse{Error: "failed to load catalog"}, nil
	}
	return wsproxygen.ListImagesCatalog200JSONResponse(imageDatasetsToGen(datasets)), nil
}

// ── CreateImageDataset ────────────────────────────────────────────────────────

func (s *templateServer) CreateImageDataset(
	ctx context.Context,
	request wsproxygen.CreateImageDatasetRequestObject,
) (wsproxygen.CreateImageDatasetResponseObject, error) {
	if !s.requireAdmin(ctx) {
		return wsproxygen.CreateImageDataset403JSONResponse{Error: "admin access required"}, nil
	}

	dataset := imageDatasetFromGen(request.Body)
	if dataset.ID == "" || dataset.Name == "" {
		return wsproxygen.CreateImageDataset400JSONResponse{Error: "id and name are required"}, nil
	}

	datasets, err := s.m.loadCatalog(ctx)
	if err != nil {
		log.Printf("syncManager: images catalog create load error: %v", err)
		return wsproxygen.CreateImageDataset503JSONResponse{Error: "failed to load catalog"}, nil
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

	if err := s.m.saveCatalog(ctx, datasets); err != nil {
		log.Printf("syncManager: images catalog create save error: %v", err)
		return wsproxygen.CreateImageDataset503JSONResponse{Error: "failed to save catalog"}, nil
	}

	genDataset := imageDatasetToGen(dataset)
	if found {
		return wsproxygen.CreateImageDataset200JSONResponse(genDataset), nil
	}
	return wsproxygen.CreateImageDataset201JSONResponse(genDataset), nil
}

// ── UpdateImageDataset ────────────────────────────────────────────────────────

func (s *templateServer) UpdateImageDataset(
	ctx context.Context,
	request wsproxygen.UpdateImageDatasetRequestObject,
) (wsproxygen.UpdateImageDatasetResponseObject, error) {
	if !s.requireAdmin(ctx) {
		return wsproxygen.UpdateImageDataset403JSONResponse{Error: "admin access required"}, nil
	}

	dataset := imageDatasetFromGen(request.Body)
	dataset.ID = request.Id

	datasets, err := s.m.loadCatalog(ctx)
	if err != nil {
		log.Printf("syncManager: images catalog update load error: %v", err)
		return wsproxygen.UpdateImageDataset503JSONResponse{Error: "failed to load catalog"}, nil
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
		return wsproxygen.UpdateImageDataset404JSONResponse{Error: "dataset not found"}, nil
	}

	if err := s.m.saveCatalog(ctx, datasets); err != nil {
		log.Printf("syncManager: images catalog update save error: %v", err)
		return wsproxygen.UpdateImageDataset503JSONResponse{Error: "failed to save catalog"}, nil
	}

	return wsproxygen.UpdateImageDataset200JSONResponse(imageDatasetToGen(dataset)), nil
}

// ── DeleteImageDataset ────────────────────────────────────────────────────────

func (s *templateServer) DeleteImageDataset(
	ctx context.Context,
	request wsproxygen.DeleteImageDatasetRequestObject,
) (wsproxygen.DeleteImageDatasetResponseObject, error) {
	auth := httpctx.AuthFrom(ctx)
	if auth.Role != apikey.RoleAdmin {
		return wsproxygen.DeleteImageDataset403JSONResponse{Error: "admin access required"}, nil
	}

	datasets, err := s.m.loadCatalog(ctx)
	if err != nil {
		log.Printf("syncManager: images catalog delete load error: %v", err)
		return wsproxygen.DeleteImageDataset503JSONResponse{Error: "failed to load catalog"}, nil
	}

	filtered := datasets[:0]
	found := false
	for _, d := range datasets {
		if d.ID == request.Id {
			found = true
		} else {
			filtered = append(filtered, d)
		}
	}
	if !found {
		return wsproxygen.DeleteImageDataset404JSONResponse{Error: "dataset not found"}, nil
	}

	if err := s.m.saveCatalog(ctx, filtered); err != nil {
		log.Printf("syncManager: images catalog delete save error: %v", err)
		return wsproxygen.DeleteImageDataset503JSONResponse{Error: "failed to save catalog"}, nil
	}

	return wsproxygen.DeleteImageDataset204Response{}, nil
}

// ── conversion helpers ────────────────────────────────────────────────────────

func imageDatasetFromGen(g *wsproxygen.ImageDataset) ImageDataset {
	d := ImageDataset{
		ID:   g.Id,
		Name: g.Name,
	}
	if g.Description != nil {
		d.Description = *g.Description
	}
	if g.ImageCount != nil {
		d.ImageCount = *g.ImageCount
	}
	if g.Category != nil {
		d.Category = *g.Category
	}
	if g.Source != nil {
		d.Source = *g.Source
	}
	if g.HuggingFaceUrl != nil {
		d.HuggingFaceURL = *g.HuggingFaceUrl
	}
	if g.Tags != nil {
		d.Tags = *g.Tags
	}
	if g.ClusterDocs != nil {
		d.ClusterDocs = *g.ClusterDocs
	}
	return d
}

func imageDatasetToGen(d ImageDataset) wsproxygen.ImageDataset {
	g := wsproxygen.ImageDataset{
		Id:   d.ID,
		Name: d.Name,
	}
	if d.Description != "" {
		g.Description = &d.Description
	}
	if d.ImageCount != 0 {
		g.ImageCount = &d.ImageCount
	}
	if d.Category != "" {
		g.Category = &d.Category
	}
	if d.Source != "" {
		g.Source = &d.Source
	}
	if d.HuggingFaceURL != "" {
		g.HuggingFaceUrl = &d.HuggingFaceURL
	}
	if len(d.Tags) > 0 {
		tags := make([]string, len(d.Tags))
		copy(tags, d.Tags)
		g.Tags = &tags
	}
	if len(d.ClusterDocs) > 0 {
		cd := make(map[string]string, len(d.ClusterDocs))
		maps.Copy(cd, d.ClusterDocs)
		g.ClusterDocs = &cd
	}
	return g
}

func imageDatasetsToGen(datasets []ImageDataset) []wsproxygen.ImageDataset {
	result := make([]wsproxygen.ImageDataset, len(datasets))
	for i, d := range datasets {
		result[i] = imageDatasetToGen(d)
	}
	return result
}
