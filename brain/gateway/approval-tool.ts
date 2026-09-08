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

// `request_approval`: the agent's way to ask the person for a platform write.
//
// The gate lives in the platform API and refuses any write made with an
// unattended credential until a human agrees. That refusal is the same on every
// path — `abx` in someone's own terminal, `abx` in this deployment's sandbox —
// and the remedy is the same too: a person decides, then the command runs
// again. What differs is only how the person is reached.
//
// In a terminal the CLI prints a link and the agent stops talking until the
// person says they clicked it. That does not work here: this agent's output
// reaches the person through a chat transcript, and asking them to leave it,
// find a page and come back is worse than showing them the decision where they
// already are. So this tool turns the refusal into a card in the conversation.
//
// The tool does NOT record the decision, and could not: the gate refuses a
// decision made with the same unattended credential that asked for it, which is
// exactly the property that makes the gate worth having. It parks the request,
// the browser records the decision with the signed-in person's own session, and
// only then does this tool run the command again.

import { createSdkMcpServer, tool } from '@anthropic-ai/claude-agent-sdk'
import { sandboxCall } from '@scitix/agentbox-hands'
import { randomBytes } from 'node:crypto'
import { z } from 'zod'
import type { AgentEvent, ApprovalAsk } from './agent-events.ts'
import type { InteractionRegistry } from './interactions.ts'
import type { AsyncQueue } from './queue.ts'

export const APPROVAL_MCP_SERVER = 'agentbox-approval'
export const APPROVAL_TOOL = 'request_approval'

/** The answer-map key the card replies under. */
export const DECISION_KEY = 'decision'

/** What the card may reply. Canonical tokens, never display text: the browser
 *  renders its own translated labels and sends one of these back, so a change
 *  of wording in one locale cannot change what the agent does. */
export const DECISION_APPROVE_ONCE = 'approve_once'
export const DECISION_APPROVE_SESSION = 'approve_session'
export const DECISION_DENY = 'deny'

interface Exec {
  exit_code: number
  stdout: string
  stderr: string
  cwd: string
}

/**
 * Pull the approval out of what a refused `abx` command printed.
 *
 * Parsing text rather than reading a field because the tool is deliberately one
 * step removed from the API: it runs whatever command the agent was refused,
 * inside the sandbox, using the person's own credential — the gateway holds no
 * credential for that cluster and should not start holding one. The trailer it
 * reads is a stable contract (`approval_block` in the CLI's renderer), not
 * incidental formatting, and every field is anchored on a token that cannot
 * appear by accident: an `apr_…` id, a `/clusters/<id>/approvals` URL.
 *
 * Returns null when the output carries no approval, which is the honest answer
 * for a command that failed for some entirely different reason.
 */
export function parseApprovalTrailer(
  output: string,
  fallbackCluster?: string
): ApprovalAsk | null {
  const id = /\bapr_[0-9a-f]+\b/.exec(output)?.[0]
  if (!id) return null

  const cluster =
    /https?:\/\/\S*?\/clusters\/([^/\s]+)\/approvals/.exec(output)?.[1] ??
    fallbackCluster ??
    ''

  // The line under the `approval:` label is the summary the platform wrote.
  const lines = output.split('\n')
  const at = lines.findIndex(l => l.trim() === 'approval:')
  const summary =
    at >= 0 && lines[at + 1] !== undefined ? lines[at + 1].trim() : ''

  // "(once)" alone means the platform will never widen this one; the scope line
  // is written by the same renderer that writes the id.
  const scope = /^\s*\((once[^)]*)\)\s*$/m.exec(output)?.[1] ?? ''
  const onceOnly = scope.trim() === 'once'

  // `error: approval required: <operation summary>` is the first line; the
  // machine name is not printed, so the summary stands in for it. Better a
  // human sentence in the header than an invented identifier.
  const operation =
    /approval required:\s*(.+)/.exec(output)?.[1]?.trim() || summary

  return {
    approvalId: id,
    cluster,
    operation,
    summary: summary || operation,
    onceOnly,
  }
}

export interface ApprovalToolDeps {
  threadId: string
  events: AsyncQueue<AgentEvent>
  interactions: InteractionRegistry
  signal: AbortSignal
  /** Cluster to name when the refusal carried no console link. */
  cluster?: string
  /** Seam for tests. Defaults to the session's sandbox daemon. */
  exec?: (command: string, cwd?: string) => Promise<Exec>
}

/** Run a command in the thread's sandbox. */
function defaultExec(threadId: string) {
  return (command: string, cwd?: string): Promise<Exec> =>
    sandboxCall<Exec>(threadId, 'bash', {
      command,
      ...(cwd ? { cwd } : {}),
      timeout_seconds: 120,
    })
}

function render(command: string, r: Exec): string {
  let out = `$ ${command}\n(cwd=${r.cwd}, exit=${r.exit_code})`
  if (r.stdout) out += `\n--- stdout ---\n${r.stdout}`
  if (r.stderr) out += `\n--- stderr ---\n${r.stderr}`
  return out
}

/**
 * The tool's whole flow, factored out of the MCP wrapper so it can be tested
 * without a harness.
 */
export async function requestApproval(
  deps: ApprovalToolDeps,
  command: string,
  cwd?: string
): Promise<string> {
  const exec = deps.exec ?? defaultExec(deps.threadId)

  const first = await exec(command, cwd)
  if (first.exit_code === 0) {
    // Nothing was gated. Returning the output rather than an apology keeps the
    // agent from running the same command a second time to see what it did.
    return (
      'This command needed no approval; it has already run.\n\n' +
      render(command, first)
    )
  }

  const ask = parseApprovalTrailer(
    `${first.stdout}\n${first.stderr}`,
    deps.cluster
  )
  if (!ask) {
    return (
      'This command did not fail for want of approval, so there is nothing to ' +
      'ask a person for. Read the error and fix the command.\n\n' +
      render(command, first)
    )
  }
  ask.command = command

  const requestId = `int_${randomBytes(8).toString('hex')}`
  const request = {
    requestId,
    kind: 'permission' as const,
    questions: [
      {
        key: DECISION_KEY,
        question: ask.summary,
        header: ask.operation,
        options: [
          { label: DECISION_APPROVE_ONCE },
          ...(ask.onceOnly ? [] : [{ label: DECISION_APPROVE_SESSION }]),
          { label: DECISION_DENY },
        ],
      },
    ],
    approval: ask,
  }
  deps.events.push({ t: 'interaction', request })

  const answers = await deps.interactions.park(
    requestId,
    deps.threadId,
    request,
    deps.signal
  )
  const decision = answers?.[DECISION_KEY] ?? ''

  if (!decision.startsWith('approve')) {
    // A refusal and a silence are reported the same way ON PURPOSE. The agent's
    // next move is identical either way — do not run this — and inventing a
    // distinction would invite it to retry the one it read as merely unanswered.
    return (
      `The command was NOT approved and has NOT run: ${ask.summary}\n` +
      'Do not try it again in this conversation unless the person asks for it. ' +
      'Tell them what you were going to do and let them decide what happens next.'
    )
  }

  const second = await exec(command, cwd)
  if (second.exit_code === 0) {
    return `Approved. The command has run.\n\n${render(command, second)}`
  }
  const again = parseApprovalTrailer(
    `${second.stdout}\n${second.stderr}`,
    deps.cluster
  )
  if (again) {
    // A fresh approval id after an approval means the retry did not match what
    // was granted — a once-grant is matched against the exact request, so a
    // command that differs even slightly asks again. Say so, because "approval
    // required" twice in a row otherwise reads as the approval not working.
    return (
      'This ran as a DIFFERENT request from the one that was approved, so the ' +
      'platform is asking again. A single approval covers the exact call it was ' +
      'granted for. Call this tool again with the same command, unchanged.\n\n' +
      render(command, second)
    )
  }
  return `Approved, but the command failed.\n\n${render(command, second)}`
}

/** The MCP server carrying the tool, for `query({ options: { mcpServers } })`. */
export function approvalMcpServer(deps: ApprovalToolDeps) {
  return createSdkMcpServer({
    name: APPROVAL_MCP_SERVER,
    version: '1.0.0',
    instructions:
      'Ask the person for permission to make a change to the platform, then ' +
      'make it. Use this whenever a command is refused with "approval ' +
      'required".',
    alwaysLoad: true,
    tools: [
      tool(
        APPROVAL_TOOL,
        'Ask the person to approve a platform command that was refused with ' +
          '"approval required", then run it for them.\n\n' +
          'Pass the command EXACTLY as you ran it. This tool runs it, shows the ' +
          'person what it would do, waits for their decision, and runs it again ' +
          'once they agree — so do not run the command yourself afterwards, and ' +
          'do not paste the approval link into the conversation: they answer ' +
          'right here.\n\n' +
          'Only for the platform CLI (`abx`). Anything else is either already ' +
          'allowed or is not something a person can approve.',
        {
          command: z
            .string()
            .describe(
              'The exact command that was refused, e.g. `abx envs create --name foo --template bar`'
            ),
          cwd: z
            .string()
            .optional()
            .describe('Working directory, if the command needs one'),
        },
        async args => ({
          content: [
            {
              type: 'text' as const,
              text: await requestApproval(deps, args.command, args.cwd),
            },
          ],
        }),
        { annotations: { readOnlyHint: false, openWorldHint: true } }
      ),
    ],
  })
}
