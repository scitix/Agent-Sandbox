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

package sandboxrender

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// ReservedVolumeNamePrefix namespaces every volume this renderer injects.
//
// Defined in the API package because rejecting a Template that uses the prefix
// is part of the API contract; re-exported here so renderer callers do not have
// to reach for the API package for a naming detail.
const ReservedVolumeNamePrefix = agentsv1alpha1.ReservedVolumeNamePrefix

// maxVolumeNameLen is the DNS-1123 label limit Kubernetes applies to
// spec.volumes[].name.
const maxVolumeNameLen = 63

// volumeNameHashLen is how many hex characters of the source digest are kept.
const volumeNameHashLen = 8

// VolumeNameFor derives the corev1.Volume name for one volume *source*.
//
// The name is:
//   - deterministic — the revision hash depends on it, so it must not vary
//     between renders of the same input;
//   - DNS-1123 label safe and at most 63 characters;
//   - distinct for the read-only and read-write forms of the same claim, which
//     must be two separate volumes because readOnly lives on the volume source.
//
// The digest is taken over the raw claim name so two claims that differ only in
// a character the sanitiser folds (e.g. "a.b" and "a-b") cannot collide.
func VolumeNameFor(claimName string, readOnly bool) string {
	mode := "rw"
	if readOnly {
		mode = "ro"
	}

	sum := sha256.Sum256([]byte(claimName + "|" + mode))
	digest := hex.EncodeToString(sum[:])[:volumeNameHashLen]

	// prefix + sanitised + "-" + mode + "-" + digest must fit in 63 chars.
	fixed := len(ReservedVolumeNamePrefix) + 1 + len(mode) + 1 + volumeNameHashLen
	budget := maxVolumeNameLen - fixed

	sanitised := sanitiseNamePart(claimName)
	if len(sanitised) > budget {
		sanitised = strings.Trim(sanitised[:budget], "-")
	}
	if sanitised == "" {
		// Degenerate claim names still produce a valid, unique name; the digest
		// carries the identity.
		return fmt.Sprintf("%s%s-%s", ReservedVolumeNamePrefix, mode, digest)
	}
	return fmt.Sprintf("%s%s-%s-%s", ReservedVolumeNamePrefix, sanitised, mode, digest)
}

// sanitiseNamePart lowercases and folds anything outside [a-z0-9-] into "-",
// collapses runs, and trims leading/trailing separators. The fixed
// ReservedVolumeNamePrefix guarantees the final name still starts with a letter,
// so this does not need to handle a leading digit.
func sanitiseNamePart(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// volumeSource identifies one corev1.Volume: PersistentVolumeClaimVolumeSource
// is exactly {claimName, readOnly}, so two entries sharing both share a volume,
// and the same claim mounted read-only at one path and read-write at another
// necessarily becomes two volumes.
type volumeSource struct {
	claimName string
	readOnly  bool
}

// applyVolumes appends the Env-level PVC mounts to the template.
//
// Volumes are keyed by source and emitted sorted by derived name; mounts are
// emitted sorted by mount path. Both orderings are load-bearing:
// ComputeRevisionHash JSON-marshals the whole Pool spec, so a non-deterministic
// order would flip the hash on every reconcile and put idle Pods into a
// permanent rebuild loop. A pure reorder of the input list is, by the same
// property, a semantic no-op that does not roll the pool.
func applyVolumes(emb *agentsv1alpha1.EmbeddedSandboxTemplate, vols []agentsv1alpha1.EnvVolumeMount) error {
	if len(emb.Template.Spec.Containers) == 0 {
		return fmt.Errorf("volumes require at least one container in the template")
	}

	names := make(map[volumeSource]string, len(vols))
	mounts := make([]corev1.VolumeMount, 0, len(vols))

	for _, v := range vols {
		src := volumeSource{claimName: v.ClaimName, readOnly: v.IsReadOnly()}
		name, ok := names[src]
		if !ok {
			name = VolumeNameFor(src.claimName, src.readOnly)
			names[src] = name
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name:      name,
			MountPath: v.MountPath,
			SubPath:   v.SubPath,
			// Mirrored onto the mount as well. kubelet computes
			// ReadOnly = mount.ReadOnly || volumeSourceReadOnly, so this can
			// only ever strengthen the result; it exists so that
			// `kubectl describe pod`, which shows the mount, is honest to a
			// human reader. Never set true when the source is read-write.
			ReadOnly: v.IsReadOnly(),
			// RecursiveReadOnly is deliberately left nil. It requires
			// Kubernetes >= 1.30 and containerd >= 2.0; on older clusters the
			// field is silently dropped when the Pod is created, which looks
			// configured end-to-end while doing nothing.
		})
	}

	sources := make([]volumeSource, 0, len(names))
	for src := range names {
		sources = append(sources, src)
	}
	sort.Slice(sources, func(i, j int) bool { return names[sources[i]] < names[sources[j]] })

	volumes := make([]corev1.Volume, 0, len(sources))
	for _, src := range sources {
		volumes = append(volumes, corev1.Volume{
			Name: names[src],
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: src.claimName,
					// The un-overridable spelling: a container cannot ask for
					// read-write on a volume whose source is read-only.
					ReadOnly: src.readOnly,
				},
			},
		})
	}

	sort.SliceStable(mounts, func(i, j int) bool { return mounts[i].MountPath < mounts[j].MountPath })

	emb.Template.Spec.Volumes = append(emb.Template.Spec.Volumes, volumes...)
	emb.Template.Spec.Containers[0].VolumeMounts = append(
		emb.Template.Spec.Containers[0].VolumeMounts, mounts...)
	return nil
}
