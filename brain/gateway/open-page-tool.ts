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

// `open_page`: put the thing being discussed on the screen.
//
// The agent works in a sandbox and answers in a chat column, while the person
// is looking at a dashboard beside it. After "I created the environment", the
// natural next move is to SHOW them — and without this the agent's only way to
// offer that is to type a URL for them to click.
//
// The tool itself navigates nothing. It records what should be opened, and the
// browser acts on the tool call it sees in the transcript. That indirection is
// what keeps this honest: the gateway has no browser, cannot know whether a
// person is even watching, and must not block a turn on a jump. It also means
// the request survives the round trip — reopening the conversation later shows
// what was opened without re-opening it.

import { createSdkMcpServer, tool } from '@anthropic-ai/claude-agent-sdk'
import { z } from 'zod'

export const NAVIGATION_MCP_SERVER = 'agentbox-navigation'
export const OPEN_PAGE_TOOL = 'open_page'

/**
 * Every page the assistant may open.
 *
 * Mirrors `OPEN_PAGE_VALUES` in the dashboard's navigation catalog, which turns
 * these into routes. A value offered here that the catalog cannot build is a
 * jump that silently does nothing, so the two are held together by a test.
 */
export const OPEN_PAGE_VALUES = [
  // Cluster-scoped lists and single-page views.
  'overview',
  'sandboxes',
  'envs',
  'templates',
  'images',
  'datasets',
  'vault',
  'approvals',
  'quota',
  'api-keys',
  'general',
  // Detail pages, which need `name` (and `pool` for a pool).
  'env',
  'pool',
  'template',
  'sandbox',
] as const

export interface OpenPageArgs {
  page: string
  cluster?: string
  name?: string
  pool?: string
}

/** What the model is told about each argument. */
const schema = {
  page: z
    .enum(OPEN_PAGE_VALUES)
    .describe(
      'Which page. Detail pages need `name`: `env` (an environment), ' +
        '`pool` (also needs `pool`), `template`, `sandbox`. The rest are ' +
        'cluster-level lists and need nothing else.'
    ),
  cluster: z
    .string()
    .optional()
    .describe('Cluster id. Defaults to the one the person is already viewing.'),
  name: z
    .string()
    .optional()
    .describe(
      'The environment name for `env` and `pool`, the template name for ' +
        '`template`, the sandbox id for `sandbox`.'
    ),
  pool: z
    .string()
    .optional()
    .describe('Pool name. Only for `page: "pool"`, alongside its env in `name`.'),
}

/**
 * Describe the jump without performing it.
 *
 * Returns the sentence the model sees. Deliberately past tense and short: the
 * page is already on screen next to it by the time the model reads this, and a
 * model told "I have opened X" stops narrating the URL.
 */
export function describeOpenPage(args: OpenPageArgs): string {
  const where = args.cluster ? ` on ${args.cluster}` : ''
  if (args.page === 'pool' && args.name && args.pool) {
    return `Opened the pool ${args.pool} in environment ${args.name}${where}.`
  }
  if (args.name) {
    return `Opened ${args.page} ${args.name}${where}.`
  }
  return `Opened the ${args.page} page${where}.`
}

/** The MCP server carrying the tool, for `query({ options: { mcpServers } })`. */
export function navigationMcpServer() {
  return createSdkMcpServer({
    name: NAVIGATION_MCP_SERVER,
    version: '1.0.0',
    instructions:
      "Show the person a page of the dashboard they are looking at. Use it " +
      'after creating or changing something, so they can see it.',
    alwaysLoad: true,
    tools: [
      tool(
        OPEN_PAGE_TOOL,
        'Open a dashboard page for the person you are talking to.\n\n' +
          'Use it right after you create or change something — an environment, ' +
          'a pool, a sandbox — so they see the result instead of reading about ' +
          'it. Their conversation with you stays open beside the page.\n\n' +
          'Say what you did in your own words as well; do not paste a URL, and ' +
          'do not describe the navigation itself. One call is enough — the page ' +
          'is already open by the time you read the result.',
        schema,
        async args => ({
          content: [
            {
              type: 'text' as const,
              text: describeOpenPage(args as OpenPageArgs),
            },
          ],
        }),
        {
          annotations: {
            // It changes nothing; it only moves what is on screen.
            readOnlyHint: true,
            openWorldHint: false,
          },
        }
      ),
    ],
  })
}
