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
	"encoding/json"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func injectionWith(header, placeholder string) agentsv1alpha1.SecretInjection {
	return agentsv1alpha1.SecretInjection{
		Credentials: []agentsv1alpha1.InjectedCredential{{
			Name:        "tok",
			ValueFrom:   agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "tok"},
			ExposeAs:    "TOKEN",
			Placeholder: placeholder,
		}},
		Rules: []agentsv1alpha1.InjectionRule{{
			Host:       "hub.example.com",
			Headers:    []agentsv1alpha1.HeaderInjection{{Name: header, Value: "Bearer {{ tok }}"}},
			Substitute: []string{"tok"},
		}},
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestRerenderInjection_NoChangeIsDetected(t *testing.T) {
	si := injectionWith("Authorization", "agbx_ph_0123456789abcdef")
	stamped := mustJSON(t, si)
	desired := si.DeepCopy()

	_, changed, err := rerenderInjection(stamped, desired)
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	if changed {
		t.Fatal("identical config must not trigger a re-push")
	}
}

// The sandbox already read its decoy from the environment; a running process
// cannot be told about a new one, so the decoy must survive a config change.
func TestRerenderInjection_PreservesExistingPlaceholder(t *testing.T) {
	stamped := mustJSON(t, injectionWith("Authorization", "agbx_ph_0123456789abcdef"))
	desired := injectionWith("X-Auth", "") // header renamed, no pinned decoy

	out, changed, err := rerenderInjection(stamped, &desired)
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	if !changed {
		t.Fatal("a renamed header must be detected as a change")
	}
	var got agentsv1alpha1.SecretInjection
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Credentials[0].Placeholder != "agbx_ph_0123456789abcdef" {
		t.Fatalf("placeholder changed to %q; the running sandbox would stop matching it",
			got.Credentials[0].Placeholder)
	}
	if got.Rules[0].Headers[0].Name != "X-Auth" {
		t.Fatalf("new header name not applied: %q", got.Rules[0].Headers[0].Name)
	}
}

// A credential added after the sandbox started cannot be exposed to it: the
// process has already read its environment. Exposing it would mint a decoy
// nothing will ever send.
func TestRerenderInjection_DropsExposeForNewCredential(t *testing.T) {
	stamped := mustJSON(t, injectionWith("Authorization", "agbx_ph_0123456789abcdef"))
	desired := injectionWith("Authorization", "agbx_ph_0123456789abcdef")
	desired.Credentials = append(desired.Credentials, agentsv1alpha1.InjectedCredential{
		Name:      "newcred",
		ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "new"},
		ExposeAs:  "NEW_TOKEN",
	})

	out, changed, err := rerenderInjection(stamped, &desired)
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	if !changed {
		t.Fatal("adding a credential is a change")
	}
	var got agentsv1alpha1.SecretInjection
	_ = json.Unmarshal([]byte(out), &got)
	for _, c := range got.Credentials {
		if c.Name == "newcred" && c.ExposeAs != "" {
			t.Fatal("a credential added mid-flight must not claim to be exposed")
		}
	}
}

// A pinned decoy on a newly added credential is honoured — the Env author chose
// a stable literal precisely so it can be known ahead of time.
func TestRerenderInjection_KeepsPinnedPlaceholderForNewCredential(t *testing.T) {
	stamped := mustJSON(t, injectionWith("Authorization", "agbx_ph_0123456789abcdef"))
	desired := injectionWith("Authorization", "agbx_ph_0123456789abcdef")
	desired.Credentials = append(desired.Credentials, agentsv1alpha1.InjectedCredential{
		Name:        "newcred",
		ValueFrom:   agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "new"},
		ExposeAs:    "NEW_TOKEN",
		Placeholder: "sk-pinned-0000000000",
	})

	out, _, err := rerenderInjection(stamped, &desired)
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	var got agentsv1alpha1.SecretInjection
	_ = json.Unmarshal([]byte(out), &got)
	for _, c := range got.Credentials {
		if c.Name == "newcred" && c.ExposeAs != "NEW_TOKEN" {
			t.Fatal("a pinned decoy should stay exposed")
		}
	}
}

func TestRerenderInjection_RejectsGarbageAnnotation(t *testing.T) {
	desired := injectionWith("Authorization", "")
	if _, _, err := rerenderInjection("{not json", &desired); err == nil {
		t.Fatal("expected a decode error")
	}
}
