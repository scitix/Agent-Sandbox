//go:build localgolden

// Local-only verification: render a ManagedAgent read from disk and print the
// resulting env so it can be diffed against a live Deployment. Not part of the
// committed test surface; run with -tags localgolden.
package managedagent

import (
	"fmt"
	"os"
	"sort"
	"testing"

	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func TestLocalGoldenRender(t *testing.T) {
	path := os.Getenv("MA_YAML")
	if path == "" {
		t.Skip("MA_YAML not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ma agentsv1alpha1.ManagedAgent
	if err := yaml.Unmarshal(raw, &ma); err != nil {
		t.Fatal(err)
	}
	r, err := Render(&ma, "checksum")
	if err != nil {
		t.Fatal(err)
	}
	c := r.Deployment.Spec.Template.Spec.Containers[0]
	var lines []string
	for _, e := range c.Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			lines = append(lines, fmt.Sprintf("%s<-%s/%s", e.Name,
				e.ValueFrom.SecretKeyRef.Name, e.ValueFrom.SecretKeyRef.Key))
		} else {
			lines = append(lines, fmt.Sprintf("%s=%s", e.Name, e.Value))
		}
	}
	sort.Strings(lines)
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	if err := os.WriteFile(os.Getenv("OUT_ENV"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	var ports []string
	for _, p := range c.Ports {
		ports = append(ports, fmt.Sprintf("%s:%d", p.Name, p.ContainerPort))
	}
	sort.Strings(ports)
	var mounts []string
	for _, m := range c.VolumeMounts {
		mounts = append(mounts, fmt.Sprintf("%s@%s[%s]", m.Name, m.MountPath, m.SubPath))
	}
	sort.Strings(mounts)
	t.Logf("ports=%v", ports)
	t.Logf("mounts=%v", mounts)
	t.Logf("envFrom=%d volumes=%d pvc=%v", len(c.EnvFrom), len(r.Deployment.Spec.Template.Spec.Volumes), r.PVC != nil)
}
