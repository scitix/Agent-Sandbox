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

/**
 * Which API operations `abx` can reach.
 *
 * Derived from the headless registry rather than maintained beside it, which
 * is the whole point: the CLI dispatches off those same entries, so "the CLI
 * registered a resource" and "the CLI reaches its operations" cannot disagree.
 * A hand-written list can be right on the day it is written and wrong by the
 * next commit, and nothing would say so.
 *
 * EXTRA_OPERATIONS is for commands that are not resource CRUD — one line each,
 * naming the command that reaches it, so an entry with no command is visible.
 */
import { operations } from '@headless/index'
import { op, type OperationKey } from './surface'

/** Non-resource commands, and the operation each one reaches. */
const EXTRA_OPERATIONS: Record<OperationKey, string> = {
  [op('GET', '/feature-gates')]: '`abx agent-context` narrows itself to the gates this deployment has on',
}

export const CLI_OPERATIONS: OperationKey[] = [
  ...operations().map((o) => op(o.method, o.path)),
  ...Object.keys(EXTRA_OPERATIONS),
]
