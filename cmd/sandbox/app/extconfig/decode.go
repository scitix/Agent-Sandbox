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

package extconfig

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// DecodeArgs converts the raw map[string]any from an ExtensionConfig into a
// concrete *T Args struct. It round-trips through YAML marshal/unmarshal so
// that the YAML field tags on T are respected.
//
// Returns a zero-value *T when raw is nil (plugin registered with no args).
func DecodeArgs[T any](raw map[string]any) (*T, error) {
	out := new(T)
	if len(raw) == 0 {
		return out, nil
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("extconfig.DecodeArgs: marshal: %w", err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("extconfig.DecodeArgs: unmarshal into %T: %w", out, err)
	}
	return out, nil
}
