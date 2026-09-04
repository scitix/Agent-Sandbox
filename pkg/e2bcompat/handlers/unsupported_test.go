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

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// allMessages is every message an unsupported operation can return.
var allMessages = map[string]string{
	"pauseResume":    msgPauseResume,
	"snapshots":      msgSnapshots,
	"fork":           msgFork,
	"templateBuild":  msgTemplateBuild,
	"templateTags":   msgTemplateTags,
	"templateAlias":  msgTemplateAlias,
	"templateListV2": msgTemplateListV2,
	"volumes":        msgVolumes,
	"clusterAdmin":   msgClusterAdmin,
	"accessTokens":   msgAccessTokens,
	"apiKeyRename":   msgAPIKeyRename,
	"secrets":        msgSecrets,
	"logs":           msgLogs,
	"metrics":        msgMetrics,
}

func TestUnsupported_Writes501WithMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := unsupportedOp("PostSandboxesSandboxIDPause", catArchitectural, msgPauseResume).write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 so callers do not read it as a transient failure, got %d", rec.Code)
	}
	var body e2bgen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not an E2B error: %v (%s)", err, rec.Body.String())
	}
	if body.Code != 501 {
		t.Fatalf("body code should match the status, got %d", body.Code)
	}
	if body.Message != msgPauseResume {
		t.Fatalf("unexpected message: %q", body.Message)
	}
}

// The message is the only thing the SDK surfaces to the caller, so each one has
// to carry a way forward rather than just a refusal. This asserts the property
// that matters — every message points somewhere — instead of pinning exact
// wording, which would make editing the copy a test failure.
func TestUnsupportedMessages_AllOfferAnAlternative(t *testing.T) {
	// Any of these reads as "do this instead". Matched case-insensitively so a
	// message starting a sentence with the phrase counts the same as one using
	// it mid-sentence.
	pointers := []string{
		"instead", "use ", "create ", "pass ", "keep it alive",
		"ask your platform admin", "start your mcp server", "read the pod logs",
		"is supported", "are supported", "send ", "delete it", "point the sandboxenv",
	}
	for name, msg := range allMessages {
		lower := strings.ToLower(msg)
		if msg == "" {
			t.Errorf("%s: empty message", name)
			continue
		}
		if strings.HasSuffix(msg, "not supported in AgentBox") {
			t.Errorf("%s: message says nothing actionable", name)
		}
		found := false
		for _, p := range pointers {
			if strings.Contains(lower, p) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: message offers no alternative: %q", name, msg)
		}
	}
}

// A message that names a capability must not name one we do not have. Guards
// against copy drifting back to suggesting pause, snapshots or volumes.
func TestUnsupportedMessages_DoNotSuggestUnsupportedThings(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)(use|call|try) (pause|resume|snapshot|fork)`)
	for name, msg := range allMessages {
		if forbidden.MatchString(msg) {
			t.Errorf("%s: message suggests an unsupported capability: %q", name, msg)
		}
	}
}

func TestStatusJSON_SendsTheHonestStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (statusJSON{status: http.StatusNotFound, msg: "template not found"}).write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var body e2bgen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not an E2B error: %v", err)
	}
	if body.Code != 404 {
		t.Fatalf("body code should match the status, got %d", body.Code)
	}
}

// --------------------------------------------------------------------------
// Create-request field rejection
// --------------------------------------------------------------------------

func TestRejectUnsupportedCreateFields(t *testing.T) {
	tr := true
	for _, tc := range []struct {
		name     string
		body     e2bgen.NewSandbox
		wantErr  bool
		mentions string
	}{
		{name: "clean request", body: e2bgen.NewSandbox{TemplateID: "env"}},
		{
			name:     "autoPause",
			body:     e2bgen.NewSandbox{TemplateID: "env", AutoPause: &tr},
			wantErr:  true,
			mentions: "timeout",
		},
		{
			name:     "autoResume",
			body:     e2bgen.NewSandbox{TemplateID: "env", AutoResume: &e2bgen.SandboxAutoResumeConfig{Enabled: true}},
			wantErr:  true,
			mentions: "pause",
		},
		{
			name:     "iam tokens",
			body:     e2bgen.NewSandbox{TemplateID: "env", Iam: &e2bgen.SandboxIam{Tokens: &e2bgen.SandboxIamTokens{"aws": {Audience: "a", TokenType: "JWT-SVID"}}}},
			wantErr:  true,
			mentions: "${e2b.secrets.",
		},
		{
			name:     "mcp",
			body:     e2bgen.NewSandbox{TemplateID: "env", Mcp: &e2bgen.Mcp{}},
			wantErr:  true,
			mentions: "MCP server",
		},
		{
			name:     "volumeMounts",
			body:     e2bgen.NewSandbox{TemplateID: "env", VolumeMounts: &[]e2bgen.SandboxVolumeMount{{}}},
			wantErr:  true,
			mentions: "SandboxEnv",
		},
		// autoPause=false is what the SDK sends by default; it must not be an error.
		{name: "autoPause false", body: func() e2bgen.NewSandbox {
			f := false
			return e2bgen.NewSandbox{TemplateID: "env", AutoPause: &f}
		}()},
		// `secure` is accepted and ignored: rejecting the SDK default would
		// break every caller for no correctness gain.
		{name: "secure", body: e2bgen.NewSandbox{TemplateID: "env", Secure: &tr}},
		// Empty maps/slices are not a request for the feature.
		{name: "empty iam tokens", body: e2bgen.NewSandbox{TemplateID: "env", Iam: &e2bgen.SandboxIam{}}},
		{name: "empty volumeMounts", body: e2bgen.NewSandbox{TemplateID: "env", VolumeMounts: &[]e2bgen.SandboxVolumeMount{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			got := rejectUnsupportedCreateFields(&body)
			if tc.wantErr && got == nil {
				t.Fatal("expected a 400 rather than silently dropping the field")
			}
			if !tc.wantErr {
				if got != nil {
					t.Fatalf("unexpected rejection: %s", got.Message)
				}
				return
			}
			if got.Code != 400 {
				t.Fatalf("expected 400, got %d", got.Code)
			}
			if !strings.Contains(got.Message, tc.mentions) {
				t.Fatalf("message should point at the alternative (%q), got %q", tc.mentions, got.Message)
			}
		})
	}
}

func TestRejectUnsupportedCreateFields_NilBody(t *testing.T) {
	if got := rejectUnsupportedCreateFields(nil); got != nil {
		t.Fatalf("nil body is handled elsewhere; got %v", got)
	}
}
