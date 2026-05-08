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

package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ConfigMapWriter writes a full ClusterConfig snapshot (clusters + host
// aliases) into a Kubernetes ConfigMap so that Worker-side components —
// ExtProc, the CrossClusterForwarder, and the in-process DNS resolver — can
// read cross-cluster routing and host-alias rules.
//
// It implements the ClusterConfigSink interface expected by the apiserver
// sync service.
type ConfigMapWriter struct {
	client    client.Client
	namespace string
	name      string
}

// NewConfigMapWriter returns a ConfigMapWriter that upserts the given ConfigMap.
func NewConfigMapWriter(c client.Client, namespace, name string) *ConfigMapWriter {
	return &ConfigMapWriter{
		client:    c,
		namespace: namespace,
		name:      name,
	}
}

// ApplyClusterConfig serialises cfg into the canonical snapshot format and
// upserts the target ConfigMap. A zero-valued snapshot (no clusters and no
// host aliases) is treated as a transient empty push and dropped so it
// cannot accidentally erase populated state on disk.
func (w *ConfigMapWriter) ApplyClusterConfig(ctx context.Context, cfg ClusterConfig) error {
	if len(cfg.Clusters) == 0 && len(cfg.HostAliases) == 0 {
		// Safety guard: never overwrite existing data with an empty snapshot.
		return nil
	}
	return WriteClusterConfig(ctx, w.client, w.namespace, w.name, cfg)
}

// WriteClusterConfig serialises cfg as YAML and upserts the ClusterConfigKey
// entry of the named ConfigMap.
//
// Behaviour:
//   - A zero-valued snapshot returns immediately without modifying the
//     ConfigMap (safe-merge guarantee).
//   - If the ConfigMap does not exist it is created.
//   - If the ConfigMap exists only the snapshot key is updated; all other
//     keys (including the legacy "clusters.yaml" written by older Managers)
//     are preserved so a Worker reader can continue to fall back during a
//     rolling upgrade.
func WriteClusterConfig(ctx context.Context, c client.Client, namespace, name string, cfg ClusterConfig) error {
	if len(cfg.Clusters) == 0 && len(cfg.HostAliases) == 0 {
		return nil
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cluster configmap writer: marshal: %w", err)
	}
	payload := string(data)

	key := client.ObjectKey{Namespace: namespace, Name: name}

	existing := &corev1.ConfigMap{}
	err = c.Get(ctx, key, existing)
	if errors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Data: map[string]string{
				ClusterConfigKey: payload,
			},
		}
		if createErr := c.Create(ctx, cm); createErr != nil {
			return fmt.Errorf("cluster configmap writer: create: %w", createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cluster configmap writer: get: %w", err)
	}

	// Skip the update when the payload hasn't changed to avoid bumping
	// resourceVersion and firing downstream informers spuriously.
	// Normalize the existing value via a round-trip unmarshal/marshal so that
	// cosmetic YAML differences (key order, whitespace) don't cause spurious writes.
	if existing.Data != nil {
		existingRaw := existing.Data[ClusterConfigKey]
		normalized := existingRaw
		var existingCfg ClusterConfig
		if err2 := yaml.Unmarshal([]byte(existingRaw), &existingCfg); err2 == nil {
			if b, err2 := yaml.Marshal(existingCfg); err2 == nil {
				normalized = string(b)
			}
		}
		if normalized == payload {
			return nil
		}
	}

	updated := existing.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[ClusterConfigKey] = payload

	patch := client.MergeFrom(existing)
	if patchErr := c.Patch(ctx, updated, patch); patchErr != nil {
		return fmt.Errorf("cluster configmap writer: patch: %w", patchErr)
	}
	return nil
}
