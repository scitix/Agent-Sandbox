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
	"fmt"

	"github.com/distribution/reference"
)

// ValidateContainerImage checks that image is a syntactically valid Docker/OCI
// image reference (e.g. "nginx:1.25", "ghcr.io/org/repo@sha256:abc...").
// It returns a *domain.AppError (400 Bad Request) on failure, nil on success.
// Empty strings are silently accepted (callers skip empty images before calling this).
func ValidateContainerImage(image string) error {
	if image == "" {
		return nil
	}
	if _, err := reference.ParseAnyReference(image); err != nil {
		return fmt.Errorf("invalid container image reference %q: %v", image, err)
	}
	return nil
}
