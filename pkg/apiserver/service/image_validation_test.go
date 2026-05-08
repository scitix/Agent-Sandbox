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
	"testing"
)

func TestValidateContainerImage_ValidImages(t *testing.T) {
	validImages := []string{
		"nginx",
		"nginx:1.25",
		"nginx:latest",
		"library/nginx:1.25",
		"ghcr.io/org/repo:v1.0.0",
		"docker.io/library/nginx:1.25",
		"registry.example.com:5000/myimage:v1",
		"myrepo/myimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"ubuntu",
		"busybox:1.36",
		"my-registry.com/my-org/my-image:v2.3.4",
	}
	for _, img := range validImages {
		if err := ValidateContainerImage(img); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", img, err)
		}
	}
}

func TestValidateContainerImage_EmptyIsAccepted(t *testing.T) {
	if err := ValidateContainerImage(""); err != nil {
		t.Errorf("expected empty string to be accepted, got error: %v", err)
	}
}

func TestValidateContainerImage_InvalidImages(t *testing.T) {
	invalidImages := []string{
		"INVALID@@IMAGE",
		"image::double-colon",
		"-starts-with-dash",
		"repo/image:tag:extra",
		"repo/image@notadigest",
	}
	for _, img := range invalidImages {
		if err := ValidateContainerImage(img); err == nil {
			t.Errorf("expected %q to be invalid, but got nil error", img)
		}
	}
}
