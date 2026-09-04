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

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// statusJSON answers an operation with a status the E2B spec does not list
// among that operation's responses.
//
// It exists for the same reason unsupported does: the generated response set
// is exactly what the spec declares, and for a few operations the spec declares
// no code for the thing that actually happened. Squeezing the answer into a
// declared code is not a harmless approximation — a missing template returned
// through the 401 response type makes the SDK raise an authentication error,
// and the caller goes off to rotate an API key that was never the problem. A
// bad argument reported as 401 does the same. Better to send the honest status
// than a declared lie.
type statusJSON struct {
	status int
	msg    string
}

func (e statusJSON) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	return json.NewEncoder(w).Encode(e2bgen.Error{Code: int32(e.status), Message: e.msg})
}

func (e statusJSON) VisitGetTemplatesTemplateIDResponse(w http.ResponseWriter) error {
	return e.write(w)
}

func (e statusJSON) VisitPostSandboxesSandboxIDTimeoutResponse(w http.ResponseWriter) error {
	return e.write(w)
}

func (e statusJSON) VisitPostApiKeysResponse(w http.ResponseWriter) error {
	return e.write(w)
}

func (e statusJSON) VisitGetSecretsResponse(w http.ResponseWriter) error { return e.write(w) }

func (e statusJSON) VisitGetSecretsSecretIDResponse(w http.ResponseWriter) error { return e.write(w) }

func (e statusJSON) VisitPostSecretsResponse(w http.ResponseWriter) error { return e.write(w) }

func (e statusJSON) VisitPostSecretsSecretIDResponse(w http.ResponseWriter) error { return e.write(w) }

func (e statusJSON) VisitDeleteSecretsSecretIDResponse(w http.ResponseWriter) error {
	return e.write(w)
}
