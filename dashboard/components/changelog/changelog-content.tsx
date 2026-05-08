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

import { CheckCircle2 } from "lucide-react"
import { MarkdownRenderer } from "@/components/changelog/markdown-renderer"
import { CHANGELOG, latestEntry } from "@/lib/changelog"

// ── Dialog variant ────────────────────────────────────────────────────────────

/**
 * Shown inside the What's New dialog — displays highlights list for the latest
 * version and the full Markdown content.
 */
export function ChangelogDialogContent() {
  const entry = latestEntry()

  if (!entry) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        No changelog entries yet.
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col gap-3">
      {/* Highlights */}
      <ul className="shrink-0 space-y-2">
        {entry.highlights.map((highlight, idx) => (
          <li key={idx} className="flex items-start gap-2">
            <CheckCircle2 className="text-brand mt-0.5 h-4 w-4 shrink-0" />
            <span className="text-foreground/80 text-sm">{highlight}</span>
          </li>
        ))}
      </ul>

      {/* Full Markdown — fills remaining height, native overflow scroll */}
      <div className="border-border max-h-[50dvh] min-h-0 flex-1 overflow-y-auto border-t pt-3">
        <MarkdownRenderer content={entry.content} compact className="pr-3" />
      </div>
    </div>
  )
}

// ── Page variant ──────────────────────────────────────────────────────────────

/**
 * Shown on the /changelog page — renders all versions in reverse chronological
 * order with full Markdown, wrapped in a ScrollArea.
 */
export function ChangelogPageContent() {
  return (
    <div className="flex flex-col divide-y">
      {CHANGELOG.map((entry) => (
        <div key={entry.version} className="px-6 py-8">
          {/* Version header */}
          <div className="mb-4 flex items-baseline gap-3">
            <span className="text-brand font-mono text-lg font-bold">v{entry.version}</span>
            <span className="text-muted-foreground font-mono text-xs">{entry.date}</span>
          </div>

          {/* Full release notes */}
          <MarkdownRenderer content={entry.content} />
        </div>
      ))}
    </div>
  )
}
