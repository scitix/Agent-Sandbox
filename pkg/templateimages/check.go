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

// Package templateimages checks the images produced by the same registry
// rewrite rules as pool rendering. It never modifies a template's spec.
package templateimages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	api "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RefreshInterval = 24 * time.Hour
	SuccessMaxAge   = 48 * time.Hour
	FailureRetry    = 15 * time.Minute
	ProbeTimeout    = 15 * time.Second
)

// Plan is a fingerprinted list of actual images, with their pod-template paths.
type Plan struct {
	Hash   string
	Images []api.TemplateImageResult
}

func Enabled(t *api.SandboxTemplate, registry *sandboxrender.RegistryRewrite) bool {
	return registry != nil && registry.Store != nil && registry.LocalClusterID != "" && api.BoolAnnotation(t, api.RegistryRewriteAnnotationKey)
}

func Build(t *api.SandboxTemplate, registry *sandboxrender.RegistryRewrite, override string) Plan {
	images := []api.TemplateImageResult{}
	add := func(field, image string) {
		if image != "" {
			images = append(images, api.TemplateImageResult{Field: field, Image: registry.Rewrite(image)})
		}
	}
	add("idleImage", t.Spec.IdleImage)
	for i, c := range t.Spec.Template.Spec.Containers {
		image := c.Image
		if i == 0 && override != "" {
			image = override
		}
		add(fmt.Sprintf("containers[%s].image", c.Name), image)
	}
	for _, c := range t.Spec.Template.Spec.InitContainers {
		add(fmt.Sprintf("initContainers[%s].image", c.Name), c.Image)
	}
	raw, _ := json.Marshal(struct {
		Cluster string
		Images  []api.TemplateImageResult
		Secrets any
	}{registry.LocalClusterID, images, t.Spec.Template.Spec.ImagePullSecrets})
	hash := sha256.Sum256(raw)
	return Plan{Hash: hex.EncodeToString(hash[:]), Images: images}
}

func FreshSuccess(s *api.TemplateImageCheck, p Plan, cluster string, now time.Time) bool {
	if s == nil || s.ClusterID != cluster || s.InputHash != p.Hash || s.CheckedAt.After(now) || now.Sub(s.CheckedAt.Time) >= SuccessMaxAge || len(s.Images) != len(p.Images) {
		return false
	}
	for _, i := range s.Images {
		if i.Reason != "Available" {
			return false
		}
	}
	return true
}

func Check(ctx context.Context, checker imageresolver.AvailabilityChecker, p Plan, cluster string, opts ...imageresolver.ResolveOption) *api.TemplateImageCheck {
	now := time.Now()
	result := &api.TemplateImageCheck{ClusterID: cluster, InputHash: p.Hash, CheckedAt: metav1.NewTime(now), NextCheckAt: metav1.NewTime(now.Add(RefreshInterval)), Images: append([]api.TemplateImageResult{}, p.Images...)}
	seen := map[string]string{}
	for i := range result.Images {
		image := &result.Images[i]
		if reason, ok := seen[image.Image]; ok {
			image.Reason = reason
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
		err := checker.CheckAvailability(probeCtx, image.Image, opts...)
		cancel()
		image.Reason = "Available"
		if err != nil {
			image.Reason = "Unavailable"
			var ae *imageresolver.AvailabilityError
			if errors.As(err, &ae) {
				image.Reason = ae.Reason
			}
			result.NextCheckAt = metav1.NewTime(now.Add(FailureRetry))
		}
		seen[image.Image] = image.Reason
	}
	return result
}
