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

// Package version holds the build-time version for all AgentBox components.
//
// The Version variable is set at compile time via:
//
//	go build -ldflags="-X github.com/scitix/agent-sandbox/pkg/version.Version=x.y.z"
//
// When not set (e.g. `go run`), it defaults to "0.0.0".
package version

// Version is the semantic version of this build.
// Set via -ldflags at compile time; defaults to "0.0.0" for development builds.
var Version = "0.0.0"
