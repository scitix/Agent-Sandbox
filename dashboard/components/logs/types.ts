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
 * What the viewer needs of a log line.
 *
 * Deliberately its own type rather than the generated `SandboxLogEntry`: the
 * lines reaching this component come from three sources with three shapes —
 * the native logs endpoint, the NDJSON stream, and the central log service via
 * the console BFF. Naming the small set of fields they agree on keeps the
 * conversion at each edge, where the differences actually are, instead of
 * teaching the viewer about all three.
 */
export interface LogEntry {
  /** RFC 3339. Empty when the source carried none — the row then shows no time. */
  timestamp: string
  log: string
  containerName?: string
  podName?: string
  namespaceName?: string
  nodeName?: string
}

/**
 * One batch of log lines, as a viewer needs it: the entries plus whether the
 * limit clipped them. `truncated` matters because a selection that resolves to
 * nothing is otherwise unexplainable — the line may simply have been cut.
 */
export interface LogQueryResult {
  entries: LogEntry[]
  total: number
  truncated: boolean
}
