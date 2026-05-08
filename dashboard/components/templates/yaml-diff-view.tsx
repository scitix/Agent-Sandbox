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

"use client"

import { parse as parseYaml, stringify as stringifyYaml } from "yaml"

import { cn } from "@/lib/utils"

// ─── LCS diff ────────────────────────────────────────────────────────────────

export type DiffLine = { type: "same" | "removed" | "added"; line: string }

function sortKeysDeep(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortKeysDeep)
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.keys(value as Record<string, unknown>)
        .sort()
        .map((k) => [k, sortKeysDeep((value as Record<string, unknown>)[k])]),
    )
  }
  return value
}

function normalizeYaml(yaml: string): string {
  if (!yaml) return yaml
  try {
    return stringifyYaml(sortKeysDeep(parseYaml(yaml)))
  } catch {
    return yaml
  }
}

export function computeYamlDiff(oldYaml: string, newYaml: string): DiffLine[] {
  const oldLines = oldYaml ? normalizeYaml(oldYaml).split("\n") : []
  const newLines = newYaml ? normalizeYaml(newYaml).split("\n") : []
  const m = oldLines.length,
    n = newLines.length
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0))
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i][j] =
        oldLines[i] === newLines[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const result: DiffLine[] = []
  let i = 0,
    j = 0
  while (i < m || j < n) {
    if (i < m && j < n && oldLines[i] === newLines[j]) {
      result.push({ type: "same", line: oldLines[i] })
      i++
      j++
    } else if (j < n && (i >= m || (dp[i + 1]?.[j] ?? 0) <= (dp[i]?.[j + 1] ?? 0))) {
      result.push({ type: "added", line: newLines[j] })
      j++
    } else {
      result.push({ type: "removed", line: oldLines[i] })
      i++
    }
  }
  return result
}

// ─── Component ───────────────────────────────────────────────────────────────

export interface YamlDiffViewProps {
  oldYaml: string
  newYaml: string
  className?: string
}

export function YamlDiffView({ oldYaml, newYaml, className }: YamlDiffViewProps) {
  const diff = computeYamlDiff(oldYaml, newYaml)

  return (
    <div
      className={cn(
        "border-border overflow-hidden rounded border font-mono text-xs leading-5",
        className,
      )}
    >
      {diff.map((d, i) => (
        <div
          key={i}
          className={cn(
            "flex items-start px-4 py-0.5",
            d.type === "removed" &&
              "bg-red-50 text-red-700 dark:bg-red-950/30 dark:text-red-400",
            d.type === "added" &&
              "bg-green-50 text-green-700 dark:bg-green-950/30 dark:text-green-400",
            d.type === "same" && "text-foreground/70",
          )}
        >
          <span className="mr-2 w-4 shrink-0 select-none text-center">
            {d.type === "removed" ? "-" : d.type === "added" ? "+" : " "}
          </span>
          <span className="whitespace-pre">{d.line}</span>
        </div>
      ))}
    </div>
  )
}
