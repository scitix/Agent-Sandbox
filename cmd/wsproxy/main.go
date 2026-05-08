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

// wsproxy is the AgentBox WebSocket proxy sidecar. All bootstrap logic
// lives in cmd/wsproxy/app so downstream distributions can import it
// without duplicating the terminal-proxy and sync-manager wiring.
package main

import wsproxy "github.com/scitix/agent-sandbox/cmd/wsproxy/app"

func main() {
	wsproxy.Run()
}
