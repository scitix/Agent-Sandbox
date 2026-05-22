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

// Factory and Registry for InstanceType Providers — verbatim shape of the
// quota.Factory registry (see pkg/framework/providers/quota/factory.go for
// the design rationale).

package instancetype

import (
	"fmt"
	"sync"

	"github.com/scitix/agent-sandbox/pkg/framework"
)

// Factory constructs an instancetype Provider from shared runtime
// dependencies and provider-specific parameters. Passing nil args is legal
// for Providers that take no parameters (e.g. Noop).
type Factory func(h framework.Handle, args framework.Args) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a Factory under the given name. Panics on duplicate.
func Register(name string, f Factory) {
	if name == "" {
		panic("instancetype.Register: empty name")
	}
	if f == nil {
		panic("instancetype.Register: nil factory for " + name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		panic("instancetype.Register: duplicate name " + name)
	}
	registry[name] = f
}

// Get looks up a registered Factory.
func Get(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("instancetype: no provider registered for %q", name)
	}
	return f, nil
}

// Build looks up a Factory by name and invokes it.
func Build(name string, h framework.Handle, args framework.Args) (Provider, error) {
	f, err := Get(name)
	if err != nil {
		return nil, err
	}
	p, err := f(h, args)
	if err != nil {
		return nil, fmt.Errorf("instancetype: factory %q: %w", name, err)
	}
	return p, nil
}
