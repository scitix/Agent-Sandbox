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

package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	api "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/templateimages"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *k8sSandboxEnvService) preflightTemplateImages(ctx context.Context, input CreateSandboxEnvInput) *domain.AppError {
	if s.imageRegistry == nil || s.imageChecker == nil {
		return nil
	}
	t := &api.SandboxTemplate{}
	reader := s.apiReader
	if reader == nil {
		reader = s.client
	}
	if err := reader.Get(ctx, client.ObjectKey{Name: input.TemplateRef.Name}, t); err != nil {
		return domain.NewInternal("Unable to read template for image validation", err)
	}
	if !templateimages.Enabled(t, s.imageRegistry) {
		return nil
	}
	override := ""
	if input.Overrides != nil {
		override = input.Overrides.Image
	}
	plan := templateimages.Build(t, s.imageRegistry, override)
	// Secret-backed credentials are namespace-specific and may have rotated;
	// never authorize them using a public/background successful cache entry.
	if input.ImagePullSecret == nil && len(t.Spec.Template.Spec.ImagePullSecrets) == 0 && templateimages.FreshSuccess(t.Status.ImageAvailability, plan, s.imageRegistry.LocalClusterID, time.Now()) {
		return nil
	}
	opts := []imageresolver.ResolveOption{imageresolver.WithPullSecrets(input.Namespace, t.Spec.Template.Spec.ImagePullSecrets)}
	if input.ImagePullSecret != nil {
		credentials := map[string]authn.AuthConfig{}
		for _, c := range input.ImagePullSecret.Registries {
			credentials[c.Registry] = authn.AuthConfig{Username: c.Username, Password: c.Password}
		}
		opts = append(opts, imageresolver.WithCredentials(credentials))
	}
	// Bound total API latency as well as each individual manifest request.
	checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result := templateimages.Check(checkCtx, s.imageChecker, plan, s.imageRegistry.LocalClusterID, opts...)
	failures := []api.TemplateImageResult{}
	missing := false
	for _, image := range result.Images {
		if image.Reason != "Available" {
			failures = append(failures, image)
			missing = missing || image.Reason == "Missing"
		}
	}
	if len(failures) == 0 {
		return nil
	}
	code := domain.ErrCodeServiceUnavailable
	biz := domain.BusinessErrorCode("TEMPLATE_IMAGE_CHECK_FAILED")
	if missing {
		code = domain.ErrCodeUnprocessableEntity
		biz = "TEMPLATE_IMAGE_NOT_FOUND"
	}
	details := struct {
		Cluster  string                    `json:"cluster"`
		Template string                    `json:"template"`
		Images   []api.TemplateImageResult `json:"images"`
	}{s.imageRegistry.LocalClusterID, t.Name, failures}
	messages := make([]string, 0, len(failures))
	for _, image := range failures {
		messages = append(messages, fmt.Sprintf("%s: %s (%s)", image.Field, image.Image, image.Reason))
	}
	return &domain.AppError{Code: code, BizCode: biz, Message: fmt.Sprintf("Template %s image check failed in cluster %s: %s", t.Name, s.imageRegistry.LocalClusterID, strings.Join(messages, "; ")), Detail: details}
}
