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

package sandboxpool

import (
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// A Pool's annotation map is not all about its Pods: it also carries the Pool's
// provenance and its autoscaler state, because that is where the Pool-scoped
// readers look for them. Copying the whole map onto every Pod (which createPod
// does, for the SI scheduler's benefit) used to stamp those onto the Pod as a
// frozen snapshot — a Pod created on 2026-09-17 carrying a
// `last-sandbox-create-time` from 2026-09-17T03:35:57Z, minutes before the Pod
// itself, and months before on a Pool that had been idle.
func TestPodAnnotationSyncDropsPoolScopedKeys(t *testing.T) {
	poolAnnotations := map[string]string{
		// Pool-scoped: must not reach the Pod.
		agentsv1alpha1.LastSandboxCreateTimeAnnotationKey:      "2026-09-17T03:35:57Z",
		agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey:    "siclaw-e2b-typescript",
		agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: "0.1.12",
		// Carried on purpose: whatever the SI scheduler reads, plus anything a
		// template author put there for the workload.
		"agentbox.navix.sh/some-scheduler-key": "kept",
		"example.com/team-default":             "kept-too",
	}

	podAnnotations := map[string]string{}
	syncAnnotations(podAnnotations, poolAnnotations, annotationsToExcludeFromPodSync)

	for _, dropped := range []string{
		agentsv1alpha1.LastSandboxCreateTimeAnnotationKey,
		agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey,
		agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey,
	} {
		if got, present := podAnnotations[dropped]; present {
			t.Errorf("Pod annotation %s = %q; a Pool-scoped key must not be copied onto Pods", dropped, got)
		}
	}
	for _, kept := range []string{"agentbox.navix.sh/some-scheduler-key", "example.com/team-default"} {
		if _, present := podAnnotations[kept]; !present {
			t.Errorf("Pod annotation %s was dropped; the exclusion must stay narrow", kept)
		}
	}
}

// The template→Pool sync has its own table for its own reason (the docs
// annotation is fetched live from the Template when a Pool detail is read, so a
// copy on the Pool would only go stale). It must keep working.
func TestTemplateAnnotationSyncStillDropsDocsKeys(t *testing.T) {
	templateAnnotations := map[string]string{
		agentsv1alpha1.SandboxTemplateDocsAnnotationKey: "docs",
		// Deprecated but still in the exclusion table, and a reader cheap to
		// keep honest.
		agentsv1alpha1.SandboxTemplatePoolDocsAnnotationKey: "pool docs", //nolint:staticcheck
		"example.com/team-default":                          "kept",
	}

	poolAnnotations := map[string]string{}
	SyncAnnotationsFromTemplate(poolAnnotations, templateAnnotations)

	if _, present := poolAnnotations[agentsv1alpha1.SandboxTemplateDocsAnnotationKey]; present {
		t.Error("docs annotation was copied onto the Pool")
	}
	if _, present := poolAnnotations[agentsv1alpha1.SandboxTemplatePoolDocsAnnotationKey]; present { //nolint:staticcheck
		t.Error("pool-docs annotation was copied onto the Pool")
	}
	if poolAnnotations["example.com/team-default"] != "kept" {
		t.Error("a template annotation that is meant to travel did not reach the Pool")
	}
}
