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
	"strings"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// The write contract, three ways it can go wrong.
//
// Create and update now take the same body (UpsertSandboxEnvRequest), which
// means the fields a client cannot change are IN the body it sends. The rule
// for those is not "absent means leave it alone": a body that has lost the
// template was built from the read shape — which is a different document — and
// saying so is the whole point of the tighter contract.

func TestUpdate_RefusesABodyThatDroppedTheTemplate(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	svc := newEnvService(t, env)

	// Exactly what reading the object and posting it back would look like if the
	// client used the read shape: no templateRef, no mode.
	_, appErr := svc.Update(context.Background(), UpdateSandboxEnvInput{
		Name:      envTestName,
		Namespace: envTestNamespace,
	})
	if appErr == nil || appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("want a bad request, got %+v", appErr)
	}
	// The useful half: it names what is in force, so the caller can put it back.
	if !strings.Contains(appErr.Message, "envd-runtime") {
		t.Fatalf("refusal should name the template in force, got %q", appErr.Message)
	}
}

func TestUpdate_RefusesChangingAFixedField(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	svc := newEnvService(t, env)

	in := updateEnvInput(env, nil)
	in.TemplateRef = &agentsv1alpha1.SandboxEnvTemplateRef{Name: "another-template"}

	_, appErr := svc.Update(context.Background(), in)
	if appErr == nil || appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("want a bad request, got %+v", appErr)
	}
	if !strings.Contains(appErr.Message, "another-template") {
		t.Fatalf("refusal should name what was asked for, got %q", appErr.Message)
	}
	// And the way out, because "you cannot do that" without a next step is a
	// dead end for an agent.
	if !strings.Contains(appErr.Message, "create a new env") {
		t.Fatalf("refusal should name the lever, got %q", appErr.Message)
	}
}

func TestUpdate_AcceptsAnEmptyFixedMapOnlyWhenThereIsNothingToLose(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	env.Labels = nil
	env.Annotations = nil
	svc := newEnvService(t, env)

	in := updateEnvInput(env, nil)
	in.Labels = nil
	in.Annotations = nil
	if _, appErr := svc.Update(context.Background(), in); appErr != nil {
		t.Fatalf("an env with no labels has nothing to preserve: %v", appErr)
	}
}

// One document serves both verbs, so a file that names a different Env is a
// mistake someone can really make — and every field in a copy-pasted file is
// legal, so nothing further down would notice. The address is the identity;
// the file's name is a cross-check.
func TestUpdate_RefusesAFileThatNamesAnotherEnv(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	svc := newEnvService(t, env)

	in := updateEnvInput(env, nil)
	other := "some-other-env"
	in.NameInFile = &other

	_, appErr := svc.Update(context.Background(), in)
	if appErr == nil || appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("want a bad request, got %+v", appErr)
	}
	// Both halves, so the caller can tell which one they meant.
	if !strings.Contains(appErr.Message, other) || !strings.Contains(appErr.Message, envTestName) {
		t.Fatalf("refusal should name the file's env and the address, got %q", appErr.Message)
	}
}

// The same file, sent to the Env it names, is accepted — which is the whole
// reason `name` is in the update body at all.
func TestUpdate_AcceptsTheNameTheCreateWrote(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	svc := newEnvService(t, env)

	in := updateEnvInput(env, nil)
	in.NameInFile = &in.Name
	if _, appErr := svc.Update(context.Background(), in); appErr != nil {
		t.Fatalf("a file naming its own env is the round trip this exists for: %v", appErr)
	}
}

// Editable is the other half: what a client exports must be a body the update
// accepts, or the round trip the export exists for does not close.
func TestEditable_IsABodyTheUpdateTakes(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Image: "registry.example/one:1"}
	svc := newEnvService(t, env)

	body, appErr := svc.Editable(context.Background(), envTestNamespace, envTestName)
	if appErr != nil {
		t.Fatalf("editable: %v", appErr)
	}
	if body.TemplateRef.Name != "envd-runtime" {
		t.Fatalf("editable should carry the template, got %q", body.TemplateRef.Name)
	}
	if body.Mode == nil || *body.Mode != gen.UpsertSandboxEnvRequestMode(env.Spec.Mode) {
		t.Fatalf("editable should carry the mode, got %v", body.Mode)
	}
	if body.Labels == nil || (*body.Labels)[agentsv1alpha1.LabelTeam] != "k8s" {
		t.Fatalf("editable should carry the labels in force, got %v", body.Labels)
	}
	// The name travels with the file. Without it the export is anonymous: a
	// caller who saves it and comes back tomorrow has nothing that says which
	// Env it is about — and the address is exactly what they no longer have.
	if body.Name == nil || *body.Name != envTestName {
		t.Fatalf("editable should carry the env's name, got %v", body.Name)
	}

	// Send it straight back, changing nothing.
	mode := gen.UpsertSandboxEnvRequestMode(env.Spec.Mode)
	if _, appErr := svc.Update(context.Background(), UpdateSandboxEnvInput{
		Name:        envTestName,
		Namespace:   envTestNamespace,
		NameInFile:  body.Name,
		TemplateRef: &agentsv1alpha1.SandboxEnvTemplateRef{Name: body.TemplateRef.Name},
		Mode:        &mode,
		Labels:      (*map[string]string)(body.Labels),
		Annotations: (*map[string]string)(body.Annotations),
	}); appErr != nil {
		t.Fatalf("the exported body must be accepted unchanged: %v", appErr)
	}
}
