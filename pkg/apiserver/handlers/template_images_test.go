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
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

type imageErrorEnvService struct {
	service.SandboxEnvService
	failure *domain.AppError
}

func (s imageErrorEnvService) Create(context.Context, service.CreateSandboxEnvInput) (*gen.SandboxEnv, *domain.AppError) {
	return nil, s.failure
}
func TestCreateEnvImageErrorHTTPStatusAndDetail(t *testing.T) {
	for _, code := range []domain.ErrorCode{domain.ErrCodeUnprocessableEntity, domain.ErrCodeServiceUnavailable} {
		s := &Server{env: imageErrorEnvService{failure: &domain.AppError{Code: code, BizCode: "TEMPLATE_IMAGE_NOT_FOUND", Message: "image unavailable", Detail: map[string]string{"image": "registry.example.com/missing:v1"}}}}
		response, err := s.CreateSandboxEnv(context.Background(), gen.CreateSandboxEnvRequestObject{Body: &gen.CreateSandboxEnvJSONRequestBody{Name: "test", TemplateRef: gen.SandboxEnvTemplateRef{Name: "test"}}})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := response.VisitCreateSandboxEnvResponse(w); err != nil {
			t.Fatal(err)
		}
		if w.Code != int(code) {
			t.Fatalf("got HTTP %d want %d", w.Code, code)
		}
		var body struct {
			ErrorCode string            `json:"errorCode"`
			Detail    map[string]string `json:"detail"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.ErrorCode != "TEMPLATE_IMAGE_NOT_FOUND" || body.Detail["image"] == "" {
			t.Fatal("structured image error lost")
		}
	}
}
