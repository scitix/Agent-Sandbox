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

package controller

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/scitix/agent-sandbox/pkg/framework"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	plugininstancetype "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	pluginquota "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// Options lets callers extend the core bootstrap without touching app.go.
// The zero value is valid and produces a minimal OSS build (Noop quota,
// no lifecycle plugins, no extra CRD types watched).
//
// Args are NOT carried here — they come from the --extension-config YAML file
// parsed after flag.Parse inside app.Run. This avoids the chicken-and-egg
// problem where Options must be constructed before flags are parsed.
type Options struct {
	// ExtraSchemeBuilders are called before ctrl.NewManager so that CRD types
	// owned by out-of-tree packages (e.g. quotav1alpha1) can be watched by the
	// informer cache.
	ExtraSchemeBuilders []func(*runtime.Scheme) error

	// OutOfTreePlugins registers SandboxPool lifecycle plugin factories.
	// Each entry's Factory is registered in the plugin registry; the Args for
	// each plugin are decoded from the --extension-config file at runtime.
	//
	// A plugin listed here is only built when its name also appears in the
	// plugins[] section of the extension config file — registering a factory
	// does not automatically enable the plugin.
	OutOfTreePlugins []PluginFactory

	// QuotaProvider registers an out-of-tree quota provider factory.
	// When nil, the Noop provider is used (quota feature disabled).
	// Args are decoded from the extension config file's quotaProvider.args field.
	//
	// Separating quota from lifecycle plugins reflects the architectural
	// distinction: a Provider is a read-only service dependency injected into
	// HTTP handlers, whereas lifecycle Plugins drive the reconciler's Pre* hooks
	// and own side-effectful operations such as resource reservation.
	QuotaProvider *QuotaProviderFactory

	// InstanceTypeProvider registers an out-of-tree InstanceType catalog
	// provider factory. When nil, the Noop provider is used (catalog disabled)
	// — Sandbox.create requests must supply raw Pod resources, and SandboxEnv
	// adoption falls back to member.InlineResources.
	// Args are decoded from the extension config file's instanceTypeProvider.args field.
	InstanceTypeProvider *InstanceTypeProviderFactory
}

// PluginFactory registers a lifecycle plugin factory along with a decoder that
// converts the raw map[string]any from the extension config into the concrete
// *Args struct the factory expects.
type PluginFactory struct {
	Name    string
	Factory plugins.Factory
	// DecodeArgs converts extconfig map[string]any → concrete framework.Args.
	// Use extconfig.DecodeArgs[MyArgs] as a one-liner implementation.
	DecodeArgs func(map[string]any) (framework.Args, error)
}

// QuotaProviderFactory registers a quota provider factory along with its
// args decoder.
type QuotaProviderFactory struct {
	Name    string
	Factory pluginquota.Factory
	// DecodeArgs converts extconfig map[string]any → concrete framework.Args.
	DecodeArgs func(map[string]any) (framework.Args, error)
}

// InstanceTypeProviderFactory registers an InstanceType catalog provider
// factory along with its args decoder.
type InstanceTypeProviderFactory struct {
	Name    string
	Factory plugininstancetype.Factory
	// DecodeArgs converts extconfig map[string]any → concrete framework.Args.
	DecodeArgs func(map[string]any) (framework.Args, error)
}
