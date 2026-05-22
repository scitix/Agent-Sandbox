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

package plugins

import (
	"fmt"
	"sync"

	"github.com/scitix/agent-sandbox/pkg/framework"
	"github.com/scitix/agent-sandbox/pkg/framework/providerset"
)

// Factory constructs a Plugin from shared runtime dependencies (Handle),
// the Provider bundle (Set), and plugin-specific parameters (Args).
//
// Plugins consume Providers from Set rather than holding their own
// ConfigMap watchers or API clients for cross-cutting concerns (catalog,
// quota, …). Passing nil args is legal for plugins that take no parameters;
// ps is always normalized (no nil fields) by Build before it reaches the
// Factory.
type Factory func(h framework.Handle, ps providerset.Set, args framework.Args) (Plugin, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a Factory under the given name. Intended to be called during
// host bootstrap or from an out-of-tree plugin's init().
//
// Panics on duplicate names — extension wiring is a startup-time concern, and
// a silent overwrite would mask a programming error.
func Register(name string, f Factory) {
	if name == "" {
		panic("plugins.Register: empty name")
	}
	if f == nil {
		panic("plugins.Register: nil factory for " + name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		panic("plugins.Register: duplicate name " + name)
	}
	registry[name] = f
}

// Get looks up a registered Factory. Returns (nil, error) if the name is
// unknown.
func Get(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("plugins: no factory registered for %q", name)
	}
	return f, nil
}

// Build is a convenience helper: look up a Factory by name and invoke it.
// Equivalent to Get(name) followed by f(h, ps, args). The Set is normalized
// (nil fields → Noop) before the Factory sees it, so plugins can dereference
// any Provider field without a nil check.
func Build(name string, h framework.Handle, ps providerset.Set, args framework.Args) (Plugin, error) {
	f, err := Get(name)
	if err != nil {
		return nil, err
	}
	p, err := f(h, ps.Normalize(), args)
	if err != nil {
		return nil, fmt.Errorf("plugins: factory %q: %w", name, err)
	}
	return p, nil
}

// reset clears the registry. Test-only helper; exported via export_test.go if
// ever needed. Keeping it unexported avoids accidental use in production.
func reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Factory{}
}

var _ = reset // silence "unused" until an export_test.go is added
