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

package quota

import (
	"context"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

const NoopName = "noop"

// Noop is a Provider that reports the quota feature as disabled. It is the
// default for open-source builds where no quota backend is configured.
type Noop struct{}

// NewNoop returns a disabled quota Provider. Safe to share.
func NewNoop() Provider { return Noop{} }

// NoopFactory is the Factory form of NewNoop — no Handle or Args are used.
// Registered at package init() so callers can Build("noop", h, nil) without
// any extra wiring.
func NoopFactory(_ framework.Handle, _ framework.Args) (Provider, error) {
	return NewNoop(), nil
}

func init() { Register(NoopName, NoopFactory) }

func (Noop) Enabled() bool { return false }

func (Noop) ListForUser(_ context.Context, _, _ string) ([]gen.Quota, *domain.AppError) {
	return nil, nil
}
