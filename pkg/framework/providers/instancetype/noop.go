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

package instancetype

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

const NoopName = "noop"

// Noop is a Provider that reports the catalog feature as disabled. It is the
// default for open-source builds where no closed-source catalog is wired in.
type Noop struct{}

// NewNoop returns a disabled Provider. Safe to share.
func NewNoop() Provider { return Noop{} }

// NoopFactory is the Factory form of NewNoop — no Handle or Args are used.
func NoopFactory(_ framework.Handle, _ framework.Args) (Provider, error) {
	return NewNoop(), nil
}

func (Noop) Enabled() bool                      { return false }
func (Noop) Get(_ string) (*InstanceType, bool) { return nil, false }
func (Noop) List() []*InstanceType              { return nil }

func (Noop) Resolve(_ context.Context, name string, _ int32) (corev1.ResourceRequirements, *domain.AppError) {
	return corev1.ResourceRequirements{}, domain.NewNotFound("instance type not found: " + name)
}

func (Noop) ResolveByResources(_ context.Context, _ corev1.ResourceRequirements) (*InstanceType, int32, *domain.AppError) {
	return nil, 0, nil
}

func init() {
	Register(NoopName, NoopFactory)
}
