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

// Codex backend — a deliberate placeholder.
//
// The seam costs nothing (the sandbox toolset and the AgentBackend interface exist
// for other reasons), but implementing it today would buy nothing, so the reasons
// live in the error message instead of in someone's memory:
//
//   1. Codex >= 0.146 removed `wire_api = "chat"`; only `responses` remains.
//      The endpoint we have serves `claude` for Anthropic models and `openai`
//      (chat) for the rest, and rejects `openai-responses` outright — so Codex
//      cannot reach ANY model available here. Verified by running `codex exec`.
//   2. The Codex TypeScript SDK exposes `approvalPolicy` as a policy string, not
//      a callback, so there is no way to ask the user a question mid-turn. That
//      makes `interaction: false`, which costs the question cards.
//   3. Thread list / rename / fork are not in the SDK (only `resumeThread`), and
//      models cannot be enumerated.
//
// What DOES look workable, for whoever picks this up: `[features] shell_tool` is a
// real switch (`codex exec --disable shell_tool`), MCP servers are configurable
// and namespaced since 0.121, and `TurnOptions.outputSchema` covers structured
// output. So bind-mcp-http.ts would be the way in.
//
// Revisit when BOTH: (a) the endpoint gains /v1/responses or GPT credentials
// appear, and (b) `codex app-server` / `codex-acp` leaves experimental with a
// human-in-the-loop path.
import {
  type AgentBackend,
  type AgentEvent,
  type BackendCapabilities,
  type ModelInfo,
  NotSupportedError,
  type ThreadInfo,
  type TranscriptEntry,
} from '../backend.ts'

const WHY =
  'codex backend is a placeholder. Blocked on: (1) codex >=0.146 requires ' +
  'wire_api="responses", but the configured endpoint serves the anthropic and ' +
  'openai-chat dialects only — no model here is reachable; (2) the Codex TS SDK ' +
  'has no permission callback, so mid-turn questions are impossible. See ' +
  'docs/assistant/agent-backend-abstraction.md section 6.'

function unsupported(): never {
  throw new NotSupportedError(WHY)
}

export class CodexBackend implements AgentBackend {
  readonly id = 'codex' as const

  // Plausible but unverified; the registry refuses to serve this backend
  // anyway, and claiming a capability we have not run would be worse.
  readonly capabilities: BackendCapabilities = {
    interaction: false,
    threadList: false,
    fork: false,
    rename: false,
    compaction: false,
    transcriptExport: false,
    reasoningStream: false,
  }

  /** The reason the registry keeps this backend out of service. */
  readonly sandboxing = 'none' as const

  async preflight(): Promise<void> {
    unsupported()
  }
  async models(): Promise<ModelInfo[]> {
    unsupported()
  }
  async listThreads(): Promise<ThreadInfo[]> {
    unsupported()
  }
  async createThread(): Promise<string> {
    unsupported()
  }
  async forkThread(): Promise<string> {
    unsupported()
  }
  async renameThread(): Promise<void> {
    unsupported()
  }
  async deleteThread(): Promise<void> {
    unsupported()
  }
  async exportThread(): Promise<TranscriptEntry[]> {
    unsupported()
  }
  run(): AsyncIterable<AgentEvent> {
    unsupported()
  }
  async interrupt(): Promise<void> {
    unsupported()
  }
  async answer(): Promise<void> {
    unsupported()
  }
}
