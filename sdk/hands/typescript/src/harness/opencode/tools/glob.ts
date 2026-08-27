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

// OpenCode override for the sandbox `glob` tool. OpenCode replaces its built-in
// with the same-named file here; all behaviour lives in ../sandbox/tools.ts so
// the Claude Code and MCP bindings cannot drift from it.
import { openCodeSandboxTool } from '../bind.ts'

export default openCodeSandboxTool('glob')
