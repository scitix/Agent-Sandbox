// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0

package instancetype

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

// NodeSelectorSnapshotAnnotation stores the server-owned placement decision on
// frozen member metadata. It is not a caller-supplied Config annotation.
const NodeSelectorSnapshotAnnotation = "agentbox.navix.sh/instance-type-node-selector"

// ValidateNodeSelector checks generic Kubernetes label syntax, without knowing
// any provider's model labels or interpreting its Extensions metadata.
func ValidateNodeSelector(selector map[string]string) error {
	for key, value := range selector {
		if errors := validation.IsQualifiedName(key); len(errors) != 0 {
			return fmt.Errorf("invalid instance node selector key %q: %v", key, errors)
		}
		if errors := validation.IsValidLabelValue(value); len(errors) != 0 {
			return fmt.Errorf("invalid instance node selector value for %q: %v", key, errors)
		}
	}
	return nil
}

// FrozenNodeSelector reads an immutable creation-time decision. Missing means a
// legacy member or an instance without placement requirements; neither is
// retroactively assigned catalog constraints during template reconciliation.
func FrozenNodeSelector(annotations map[string]string) (map[string]string, error) {
	raw, exists := annotations[NodeSelectorSnapshotAnnotation]
	if !exists {
		return nil, nil
	}
	var selector map[string]string
	if err := json.Unmarshal([]byte(raw), &selector); err != nil {
		return nil, fmt.Errorf("invalid frozen instance node selector: %w", err)
	}
	if len(selector) == 0 {
		return nil, fmt.Errorf("frozen instance node selector must not be empty")
	}
	if err := ValidateNodeSelector(selector); err != nil {
		return nil, err
	}
	return selector, nil
}
