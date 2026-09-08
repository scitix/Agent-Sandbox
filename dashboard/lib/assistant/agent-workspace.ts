/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Where the agent's per-user workspace lives.
//
// One fixed path, because it has to be the same string in five places at once:
// the gateway hands it to whichever harness runs the turn, the browser posts it
// with every attachment and workspace listing, the workspace-fs server accepts
// paths only under it, the sandbox creates it as the agent's cwd, and the image
// pre-creates it so an unprivileged process can mkdir a user's dir inside it.
//
// It is deliberately NOT configurable. It used to be `NAVIX_USER_DIR_ROOT`, which
// was worse than a constant: most consumers ignored the variable, so setting it
// moved the gateway while the fs server went on rejecting the new path (400) and
// the browser went on posting the old one. A knob only half the consumers honour
// is a trap, and nothing about a deployment wants this path to differ.
//
// The name mentions no harness and no product: it is visible to the model as its
// cwd, and the agent has no business reading which harness or which vendor is
// running it off a directory name.
//
// Consumers that cannot import this module carry the literal and must be changed
// with it (there is no runtime that would catch the drift):
//   oss/assistant/proxy_daemon/fs.py                  ROOT (ALLOWED derives from it)
//   oss/assistant/entrypoint.sh                       mkdir of the root
//   oss/assistant/Dockerfile, assistant/Dockerfile    mkdir + chown of the root
//   oss/assistant/vendor/langfuse-observability/index.ts  userId prefix guard
//   pkg/scitix/proxy/diagnosis/triage.go              botUserDirRoot (legacy path)
export const USER_DIR_ROOT = '/home/agents/u'

/**
 * The per-user identity directory: the agent's cwd, the harness's session
 * namespace, and the key attachment staging is filed under. All three must agree
 * or a staged file flushes into a directory the agent never looks at.
 */
export function userDirectory(userKey: string): string {
  return `${USER_DIR_ROOT}/${userKey}`
}
