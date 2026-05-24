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
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// ValidateContainerImage is a thin alias kept so service-package callers can
// stay against the existing symbol; the implementation lives in
// pkg/sandboxrender so the controller layer can validate without taking a
// service-package dependency.
func ValidateContainerImage(image string) error {
	return sandboxrender.ValidateContainerImage(image)
}
