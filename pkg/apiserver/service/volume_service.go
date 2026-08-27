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
	"maps"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// VolumeService lists the PersistentVolumeClaims a caller may mount into a
// sandbox.
type VolumeService interface {
	// List returns the Bound claims in namespace. The caller passes the
	// namespace their identity resolved to; there is no way for an API client
	// to name a different one, which is what makes this the authorisation
	// boundary rather than a filter.
	List(ctx context.Context, namespace string) ([]gen.VolumeItem, *domain.AppError)
}

type k8sVolumeService struct {
	// reader is uncached on purpose. The manager's delegating client starts an
	// informer lazily per object kind on first access, so one cached read here
	// would begin mirroring every PersistentVolumeClaim in the cluster into the
	// operator's memory for the lifetime of the process. Per-namespace claim
	// counts are small, so reading through to the API server is cheaper than the
	// cache it would otherwise build.
	reader client.Reader
	cfg    VolumeConfig
}

// NewVolumeService constructs the default VolumeService. reader should be an
// uncached reader; see the field comment. cfg supplies the display-name label
// keys and the feature gate.
func NewVolumeService(reader client.Reader, cfg VolumeConfig) VolumeService {
	return &k8sVolumeService{reader: reader, cfg: cfg}
}

func (s *k8sVolumeService) List(ctx context.Context, namespace string) ([]gen.VolumeItem, *domain.AppError) {
	// While the feature is off there is nothing a caller could do with the
	// answer — overrides.volumes is rejected — so report an empty catalog rather
	// than enumerate a namespace's storage.
	if !s.cfg.Enabled || namespace == "" {
		return []gen.VolumeItem{}, nil
	}

	list := &corev1.PersistentVolumeClaimList{}
	if err := s.reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	out := make([]gen.VolumeItem, 0, len(list.Items))
	for i := range list.Items {
		pvc := &list.Items[i]
		// Only Bound claims are mountable: an unbound claim leaves every warm
		// Pod in the pool Pending, which is a far worse outcome than not
		// offering it.
		if pvc.Status.Phase != corev1.ClaimBound {
			continue
		}
		out = append(out, s.volumeItem(pvc))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClaimName < out[j].ClaimName })
	return out, nil
}

// volumeItem projects one claim onto the wire shape.
func (s *k8sVolumeService) volumeItem(pvc *corev1.PersistentVolumeClaim) gen.VolumeItem {
	item := gen.VolumeItem{
		ClaimName:   pvc.Name,
		Phase:       string(pvc.Status.Phase),
		DisplayName: ptr.To(s.displayName(pvc)),
	}

	// Prefer the bound capacity over the request: it is what the sandbox
	// actually gets. Fall back to the request when status is not populated.
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		item.Capacity = ptr.To(q.String())
	} else if q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		item.Capacity = ptr.To(q.String())
	}

	if len(pvc.Spec.AccessModes) > 0 {
		modes := make([]string, 0, len(pvc.Spec.AccessModes))
		for _, m := range pvc.Spec.AccessModes {
			modes = append(modes, string(m))
		}
		item.AccessModes = &modes
	}
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		item.StorageClass = pvc.Spec.StorageClassName
	}
	if len(pvc.Labels) > 0 {
		labels := make(map[string]string, len(pvc.Labels))
		maps.Copy(labels, pvc.Labels)
		item.Labels = &labels
	}
	return item
}

// displayName resolves a human-readable name from the configured label keys,
// first non-empty value winning, falling back to the claim name.
//
// The label vocabulary is configuration rather than code because it belongs to
// whichever storage platform is deployed; hardcoding a vendor's keys here would
// put an internal naming scheme into a public repository and would break the
// next deployment that names things differently.
func (s *k8sVolumeService) displayName(pvc *corev1.PersistentVolumeClaim) string {
	for _, key := range s.cfg.DisplayNameLabels {
		if v, ok := pvc.Labels[key]; ok && v != "" {
			return v
		}
	}
	return pvc.Name
}
