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

// Package k8sname provides validation for Kubernetes resource names
// with a stricter variant of RFC 1123 DNS label rules: names must start
// with a lowercase letter (not a digit).
package k8sname

import (
	"fmt"
	"regexp"
)

// dns1123LabelRe matches valid K8s resource names:
//   - Must start with a lowercase letter [a-z]
//   - May continue with lowercase letters, digits, or hyphens [a-z0-9-]
//   - Must end with a lowercase letter or digit [a-z0-9]
//   - Single-character names (a single letter) are valid via the `?` quantifier
var dns1123LabelRe = regexp.MustCompile(`^[a-z]([a-z0-9-]*[a-z0-9])?$`)

const maxNameLen = 63

// Validate checks that name conforms to RFC 1123 DNS label rules with the
// additional constraint that it must start with a lowercase letter (not a digit).
// Returns nil on success, or a descriptive error.
func Validate(name string) error {
	if len(name) > maxNameLen {
		return fmt.Errorf("name %q must be at most %d characters", name, maxNameLen)
	}
	if !dns1123LabelRe.MatchString(name) {
		return fmt.Errorf("name %q is invalid: must start with a lowercase letter, end with alphanumeric, and contain only [a-z0-9-]", name)
	}
	return nil
}
