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

import (
	"os"
	"strings"
)

// EnvVersion overrides the compiled-in version at startup. Deployments set it
// from the container image tag, which answers the question an operator is
// actually asking when they read a version off the console: which build is
// running right now. A semver alone cannot say that — several images ship under
// one version number — and it is only as accurate as the build's -ldflags.
const EnvVersion = "AGENTBOX_VERSION"

// Version is the semantic version of this build.
// Set via -ldflags at compile time; defaults to "0.0.0" for development builds.
var Version = "0.0.0"

// Resolve returns the version to report to clients: the AGENTBOX_VERSION
// environment variable when set, otherwise the compiled-in Version.
//
// The result is free-form — an image tag like "develop-abc1234" is a valid
// answer. Nothing parses it as semver: it travels only in the
// X-AgentBox-Server-Version response header, which clients display and probe
// for presence. Client version negotiation compares a different header against
// its own constant (pkg/apiserver/router/middleware/version.go).
func Resolve() string {
	if v := strings.TrimSpace(os.Getenv(EnvVersion)); v != "" {
		return v
	}
	return Version
}
