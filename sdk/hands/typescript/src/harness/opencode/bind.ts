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

// OpenCode binding for the sandbox toolset.
//
// OpenCode discovers tools by FILENAME under its config dir, and this binding is
// written on the premise that a file whose name matches a built-in REPLACES that
// built-in. So the override mechanism is the `tools/<name>.ts` files themselves;
// each is a one-liner that asks this module for the binding, and all behaviour
// lives in core/tools.ts.
//
// THAT PREMISE IS UNVERIFIED against OpenCode 1.18.16, where the override appears
// to register alongside the built-in rather than in place of it. If the built-in
// wins, an agent's shell runs on the host instead of in the sandbox — the exact
// failure this package exists to prevent. See "The OpenCode override may not be
// replacing anything" in sdk/hands/README.md for the measurements and for what
// answering it takes. Nothing below is wrong either way; what is in question is
// whether installing these files is sufficient.
//
// This path is the production fallback while Claude Code is being evaluated, so
// it must stay behaviourally identical to what shipped before the extraction:
// same descriptions, same argument names, same rendered output, same notice
// placement. `tool.schema` is literally zod (`typeof z`), so the descriptor is
// built with OpenCode's own copy — never ours — and two zod instances can never
// disagree about `instanceof`.
import { tool } from '@opencode-ai/plugin'

import {
  type SchemaBuilder,
  type SchemaFactory,
  toSchemaShape,
} from '../../core/params.ts'
import {
  type SandboxTool,
  type SandboxToolName,
  renderToolResult,
  sandboxToolset,
} from '../../core/tools.ts'

type OpenCodeSchema = ReturnType<typeof tool.schema.string>

// zod's builders return `this`-typed values, which is compatible with the
// structural SchemaFactory but not inferrable through it; assert once here
// rather than at each of the seven call sites.
const factory = tool.schema as unknown as SchemaFactory<OpenCodeSchema> & {
  string(): SchemaBuilder<OpenCodeSchema>
}

function bind(t: SandboxTool) {
  return tool({
    description: t.description,
    args: toSchemaShape(t.params, factory) as Parameters<
      typeof tool
    >[0]['args'],
    async execute(args, ctx) {
      // OpenCode's session id IS the sandbox key on this path: the daemon has
      // always been addressed by it, and existing sessions must keep resolving
      // to the sandbox they already own.
      const result = await t.run(args as Record<string, unknown>, {
        sessionKey: ctx.sessionID,
      })
      return renderToolResult(result)
    },
  })
}

const BY_NAME = new Map(sandboxToolset().map(t => [t.name, bind(t)]))

/** The OpenCode tool object for one sandbox tool, for `tools/<name>.ts`. */
export function openCodeSandboxTool(name: SandboxToolName) {
  const bound = BY_NAME.get(name)
  if (!bound) throw new Error(`unknown sandbox tool: ${name}`)
  return bound
}
