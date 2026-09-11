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
 * Hand-maintained only until the headless package owns the CLI's resource
 * definitions; then this becomes a projection of those and stops being a place
 * anyone edits. Until then it is deliberately a flat list rather than anything
 * clever, so that "the CLI cannot do this yet" is a visible line in a diff.
 */
import { op, type OperationKey } from './surface'

export const CLI_OPERATIONS: OperationKey[] = [
  // Reads — the seven resource kinds the CLI registers today.
  op('GET', '/clusters'),
  op('GET', '/envs'),
  op('GET', '/envs/{name}'),
  op('GET', '/envs/{name}/sandboxpools'),
  op('GET', '/envs/{name}/events'),
  op('GET', '/envs/{name}/sandboxpools/{poolName}'),
  op('GET', '/instancetypes'),
  op('GET', '/quotas'),
  op('GET', '/sandboxes'),
  op('GET', '/sandboxes/{sandboxId}'),
  op('GET', '/sandbox-templates'),
  op('GET', '/sandbox-templates/{name}'),

  // Writes — the only two that exist.
  op('POST', '/envs'),
  op('POST', '/envs/{name}/sandboxpools'),
]
