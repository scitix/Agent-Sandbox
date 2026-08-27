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

package v1alpha1

import (
	"fmt"
	"path"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ReservedVolumeNamePrefix namespaces every volume the Pool renderer injects
// for an Env-declared PVC mount.
//
// A Template that declares a volume with this prefix is rejected on write. That
// rejection is what makes the naming guarantee hold: an injected volume can
// never collide with a Template-declared one, or with one a plugin invents
// later, without anybody maintaining a list of reserved names.
const ReservedVolumeNamePrefix = "abx-vol-"

// maxEnvVolumeMounts caps declared mounts. Also enforced by a kubebuilder
// marker on EnvOverridesSpec.Volumes; duplicated here because validation runs
// on objects built in Go, before the API server would apply the schema.
const maxEnvVolumeMounts = 8

// maxEnvVolumeSources caps distinct (claimName, readOnly) pairs. Each pair is
// one corev1.Volume and therefore one NodePublishVolume call on Pod creation,
// which on a network filesystem costs real time in pool warm-up.
const maxEnvVolumeSources = 4

// forbiddenMountPrefixes are paths a sandbox must not have shadowed by a
// user-supplied mount. /var/lib/kubelet is the CSI staging directory; the rest
// would break the container outright.
var forbiddenMountPrefixes = []string{
	"/proc",
	"/sys",
	"/dev",
	"/etc",
	"/var/lib/kubelet",
	"/var/run",
}

// forbiddenExactMounts are paths that may not be mounted directly but whose
// children are fine. /mnt is where the platform injects its own tooling
// (/mnt/agentbox), so taking /mnt itself would shadow it.
var forbiddenExactMounts = []string{"/", "/mnt"}

// ValidateVolumeMounts checks Env-declared PVC mounts for anything that would
// produce a broken or dangerous Pod.
//
// tmplSpec is the rendered Template's pod spec and may be nil; when supplied,
// mounts are additionally checked against what the Template already declares,
// which is what keeps a user from shadowing /mnt/agentbox or a DinD template's
// /var/lib/docker without either path being named here.
//
// It is called when a SandboxEnv is written, so the author sees the error, and
// again from the renderer, so a hand-edited CR cannot slip through.
func ValidateVolumeMounts(vols []EnvVolumeMount, tmplSpec *corev1.PodSpec) error {
	if len(vols) == 0 {
		return nil
	}
	if len(vols) > maxEnvVolumeMounts {
		return fmt.Errorf("at most %d volume mounts are allowed, got %d", maxEnvVolumeMounts, len(vols))
	}

	if tmplSpec != nil {
		if err := validateTemplateVolumeHygiene(tmplSpec); err != nil {
			return err
		}
	}

	seenPaths := make([]string, 0, len(vols))
	sources := make(map[string]struct{}, len(vols))

	for i, v := range vols {
		where := fmt.Sprintf("volumes[%d]", i)

		if v.ClaimName == "" {
			return fmt.Errorf("%s.claimName is required", where)
		}
		if errs := validation.IsDNS1123Subdomain(v.ClaimName); len(errs) > 0 {
			return fmt.Errorf("%s.claimName %q is not a valid PersistentVolumeClaim name: %s",
				where, v.ClaimName, strings.Join(errs, "; "))
		}

		if err := validateMountPath(where, v.MountPath); err != nil {
			return err
		}
		if err := validateSubPath(where, v.SubPath); err != nil {
			return err
		}

		// Duplicate or nested mount paths among the declared set. Nesting is
		// legal in Kubernetes but order-dependent and confusing, so it is
		// refused rather than silently ordered.
		for _, prev := range seenPaths {
			if prev == v.MountPath {
				return fmt.Errorf("%s.mountPath %q is declared more than once", where, v.MountPath)
			}
			if isPathWithin(v.MountPath, prev) || isPathWithin(prev, v.MountPath) {
				return fmt.Errorf("%s.mountPath %q nests with another declared mount %q; "+
					"declare sibling paths instead", where, v.MountPath, prev)
			}
		}
		seenPaths = append(seenPaths, v.MountPath)

		if tmplSpec != nil {
			if err := validateAgainstTemplateMounts(where, v.MountPath, tmplSpec); err != nil {
				return err
			}
		}

		mode := "rw"
		if v.IsReadOnly() {
			mode = "ro"
		}
		sources[v.ClaimName+"|"+mode] = struct{}{}
	}

	if len(sources) > maxEnvVolumeSources {
		return fmt.Errorf("at most %d distinct volume sources are allowed, got %d "+
			"(the same claim mounted both read-only and read-write counts as two)",
			maxEnvVolumeSources, len(sources))
	}
	return nil
}

// validateMountPath enforces the shape of a container mount path.
func validateMountPath(where, p string) error {
	if p == "" {
		return fmt.Errorf("%s.mountPath is required", where)
	}
	if !path.IsAbs(p) {
		return fmt.Errorf("%s.mountPath %q must be an absolute path", where, p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("%s.mountPath %q is not a clean path (want %q)", where, p, path.Clean(p))
	}
	if slices.Contains(forbiddenExactMounts, p) {
		return fmt.Errorf("%s.mountPath %q may not be mounted directly", where, p)
	}
	for _, bad := range forbiddenMountPrefixes {
		if p == bad || isPathWithin(p, bad) {
			return fmt.Errorf("%s.mountPath %q is inside the reserved path %q", where, p, bad)
		}
	}
	return nil
}

// validateSubPath enforces the shape of a volume subPath. Kubernetes rejects
// absolute paths and backsteps itself; refusing them here turns a Pod-creation
// failure minutes later into a 400 now.
func validateSubPath(where, sp string) error {
	if sp == "" {
		return nil
	}
	if path.IsAbs(sp) {
		return fmt.Errorf("%s.subPath %q must be relative to the volume root", where, sp)
	}
	if path.Clean(sp) != sp {
		return fmt.Errorf("%s.subPath %q is not a clean path (want %q)", where, sp, path.Clean(sp))
	}
	if slices.Contains(strings.Split(sp, "/"), "..") {
		return fmt.Errorf("%s.subPath %q may not contain %q segments", where, sp, "..")
	}
	return nil
}

// validateAgainstTemplateMounts refuses a mount path that collides with, or
// nests against, a path the Template already mounts in any container.
func validateAgainstTemplateMounts(where, p string, spec *corev1.PodSpec) error {
	check := func(containers []corev1.Container, kind string) error {
		for _, c := range containers {
			for _, m := range c.VolumeMounts {
				switch {
				case m.MountPath == p:
					return fmt.Errorf("%s.mountPath %q is already mounted by %s %q",
						where, p, kind, c.Name)
				case isPathWithin(p, m.MountPath):
					return fmt.Errorf("%s.mountPath %q is inside %q, already mounted by %s %q",
						where, p, m.MountPath, kind, c.Name)
				case isPathWithin(m.MountPath, p):
					return fmt.Errorf("%s.mountPath %q would shadow %q, mounted by %s %q",
						where, p, m.MountPath, kind, c.Name)
				}
			}
		}
		return nil
	}
	if err := check(spec.Containers, "container"); err != nil {
		return err
	}
	return check(spec.InitContainers, "init container")
}

// validateTemplateVolumeHygiene rejects Templates that would undermine the
// renderer's guarantees.
func validateTemplateVolumeHygiene(spec *corev1.PodSpec) error {
	for _, v := range spec.Volumes {
		if strings.HasPrefix(v.Name, ReservedVolumeNamePrefix) {
			return fmt.Errorf("template volume %q uses the reserved prefix %q, "+
				"which is owned by Env-declared volume mounts", v.Name, ReservedVolumeNamePrefix)
		}
	}
	// recursiveReadOnly exists in the Go type but requires Kubernetes >= 1.30
	// and containerd >= 2.0. On an older cluster the field is silently dropped
	// when the Pod is created, so a template carrying it looks protected while
	// being exactly as exposed as one that omits it. Refuse it rather than let
	// an author believe otherwise.
	all := append(slices.Clone(spec.Containers), spec.InitContainers...)
	for _, c := range all {
		for _, m := range c.VolumeMounts {
			if m.RecursiveReadOnly != nil {
				return fmt.Errorf("container %q mount %q sets recursiveReadOnly, which requires "+
					"Kubernetes >= 1.30 and containerd >= 2.0 and is silently dropped otherwise; "+
					"use a read-only Env volume mount instead (readOnly lands on the volume source)",
					c.Name, m.MountPath)
			}
		}
	}
	return nil
}

// isPathWithin reports whether child is strictly inside parent. Both are
// expected to be clean absolute paths.
func isPathWithin(child, parent string) bool {
	if parent == "/" {
		return child != "/"
	}
	return strings.HasPrefix(child, parent+"/")
}

// ReadOnlyDefeatingFeatures reports pod-spec features that let a container
// reach the host mount namespace and remount a read-only bind mount
// read-write.
//
// Read-only on a PVC is enforced by the container runtime, not by the CSI
// driver: the driver mounts the backing filesystem read-write on the host and
// kubelet asks the runtime for a read-only bind. Anything that can act in the
// host mount namespace therefore bypasses it.
//
// Returns a human-readable list, empty when the spec cannot defeat read-only.
func ReadOnlyDefeatingFeatures(spec *corev1.PodSpec) []string {
	if spec == nil {
		return nil
	}
	var out []string

	// hostPID / hostIPC do not by themselves allow a remount, but a pod sharing
	// host namespaces is not isolated from the node, and combined with a
	// capability grant it becomes a path to the host mount table. Surface them.
	if spec.HostPID {
		out = append(out, "hostPID")
	}
	if spec.HostIPC {
		out = append(out, "hostIPC")
	}

	for _, v := range spec.Volumes {
		if v.HostPath != nil {
			out = append(out, fmt.Sprintf("hostPath volume %q (%s)", v.Name, v.HostPath.Path))
		}
	}

	inspect := func(containers []corev1.Container, kind string) {
		for _, c := range containers {
			// mountPropagation lives on the mount, not the securityContext, so
			// it must be checked even for a container that declares no
			// securityContext at all.
			for _, m := range c.VolumeMounts {
				if m.MountPropagation != nil && *m.MountPropagation == corev1.MountPropagationBidirectional {
					out = append(out, fmt.Sprintf("%s %q mounts %q with Bidirectional propagation",
						kind, c.Name, m.MountPath))
				}
			}
			sc := c.SecurityContext
			if sc == nil {
				continue
			}
			if sc.Privileged != nil && *sc.Privileged {
				out = append(out, fmt.Sprintf("%s %q is privileged", kind, c.Name))
			}
			if sc.ProcMount != nil && *sc.ProcMount == corev1.UnmaskedProcMount {
				out = append(out, fmt.Sprintf("%s %q sets procMount=Unmasked", kind, c.Name))
			}
			if sc.Capabilities != nil {
				for _, capAdd := range sc.Capabilities.Add {
					switch capAdd {
					case "SYS_ADMIN", "ALL":
						out = append(out, fmt.Sprintf("%s %q adds capability %s", kind, c.Name, capAdd))
					}
				}
			}
		}
	}
	inspect(spec.Containers, "container")
	inspect(spec.InitContainers, "init container")

	return out
}

// ValidateReadOnlyEnforceable refuses a read-only volume declaration that the
// pod spec cannot actually honour.
//
// Scoped deliberately to read-only entries: an entry the user marked writable
// never claimed a guarantee, so a privileged Docker-in-Docker template mounting
// a scratch dataset read-write is unaffected. That narrowing is what keeps the
// rule from breaking legitimate privileged templates.
//
// allowed comes from the Template's admin-only opt-out annotation. Template
// writes are admin-scoped on purpose: whoever chose `privileged: true` is the
// one who may accept its consequence, not the user mounting the dataset.
func ValidateReadOnlyEnforceable(vols []EnvVolumeMount, spec *corev1.PodSpec, allowed bool) error {
	if allowed || len(vols) == 0 {
		return nil
	}
	var readOnly []string
	for _, v := range vols {
		if v.IsReadOnly() {
			readOnly = append(readOnly, v.MountPath)
		}
	}
	if len(readOnly) == 0 {
		return nil
	}
	defeats := ReadOnlyDefeatingFeatures(spec)
	if len(defeats) == 0 {
		return nil
	}
	return fmt.Errorf("read-only volume mounts %v cannot be enforced on this template: %s. "+
		"Read-only is a container-runtime bind flag, and the backing filesystem is mounted "+
		"read-write on the host, so a pod with host mount-namespace access can remount it. "+
		"Either drop those pod-spec features, mark the mounts readOnly: false to accept the risk "+
		"explicitly, or have an administrator set the %s annotation on the template",
		readOnly, strings.Join(defeats, "; "), AllowUnenforceableReadOnlyVolumesAnnotationKey)
}
