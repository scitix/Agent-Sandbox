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

package framework

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Args is the extension-specific parameter object passed to a Factory.
//
// Each extension declares a concrete *MyArgs struct and asserts it inside its
// Factory body:
//
//	type MyArgs struct{ URL string }
//
//	func Factory(h framework.Handle, ps providerset.Set, args framework.Args) (Plugin, error) {
//	    if args == nil {
//	        return nil, errors.New("myplugin: args required")
//	    }
//	    a, ok := args.(*MyArgs)
//	    if !ok {
//	        return nil, fmt.Errorf("myplugin: got %T, want *MyArgs", args)
//	    }
//	    // use a.URL, ps.Quota, ps.InstanceType …
//	}
//
// Passing nil is legal — use it for extensions that take no parameters.
type Args any

// DefaultHandle is a ready-made Handle implementation used by cmd wiring and
// tests. Fields are public so callers can build the struct inline; zero
// values are valid (accessors return the zero value of their declared type).
type DefaultHandle struct {
	C      client.Client
	Cch    cache.Cache
	Logger logr.Logger
}

var _ Handle = (*DefaultHandle)(nil)

func (h *DefaultHandle) Client() client.Client { return h.C }
func (h *DefaultHandle) Cache() cache.Cache    { return h.Cch }
func (h *DefaultHandle) Log() logr.Logger      { return h.Logger }
