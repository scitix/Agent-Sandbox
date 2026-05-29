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

// ─── Chart color tokens ───────────────────────────────────────────────────
// Named semantic colors for use in series={[{ name: "P99", color: C.red }]}.
// Keeps hex values in one place; names stay in the call site.

export const C = {
  // Base palette
  green: "#22c55e",
  red: "#ef4444",
  amber: "#f59e0b",
  orange: "#f97316",
  yellow: "#eab308",
  blue: "#3b82f6",
  indigo: "#6366f1",
  violet: "#8b5cf6",
  purple: "#a855f7",
  pink: "#ec4899",
  teal: "#14b8a6",
  cyan: "#06b6d4",
  lime: "#84cc16",
  emerald: "#10b981",

  // Semantic aliases — use these when the color carries domain meaning
  // Latency percentiles: severity descending red → orange → yellow → blue
  p99: "#ef4444",
  p95: "#f97316",
  p90: "#eab308",
  p50: "#3b82f6",

  // Network direction
  rx: "#8b5cf6",
  tx: "#14b8a6",

  // Replica / sandbox states
  desired: "#6366f1",
  running: "#22c55e",
  starting: "#f59e0b",
  stopping: "#f97316",
  idle: "#3b82f6",

  // Outcome / health
  success: "#22c55e",
  warning: "#f59e0b",
  error: "#ef4444",
  canceled: "#a855f7",
  completed: "#6366f1",
  released: "#f59e0b",
} as const
