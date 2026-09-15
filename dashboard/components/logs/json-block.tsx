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

/**
 * A highlighted JSON block for the log detail panel.
 *
 * Small on purpose. The panel shows one field's nested JSON at a time — a
 * request body, a set of labels — never a file, so none of a full code viewer's
 * machinery (line numbers, folding, virtualization, copy-per-line) applies.
 *
 * Highlighting reuses the dual-theme Shiki setup the Markdown renderer already
 * loads: it emits `--shiki-light` / `--shiki-dark` custom properties, and
 * app/globals.css picks between them off the `.dark` class, so the block
 * follows the app theme with no JavaScript involved. Until the highlighter
 * resolves (and if it fails), the raw text renders in the same box — a log
 * viewer that shows nothing while waiting on syntax colour would have its
 * priorities backwards.
 */

import { useEffect, useState } from "react"
import { getSingletonHighlighter } from "shiki"
import { cn } from "@/lib/utils"

const highlighterPromise = getSingletonHighlighter({
  themes: ["github-light", "github-dark"],
  langs: ["json"],
})

export function JsonBlock({ code, className }: { code: string; className?: string }) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void highlighterPromise
      .then((hl) =>
        hl.codeToHtml(code, {
          lang: "json",
          themes: { light: "github-light", dark: "github-dark" },
          defaultColor: false,
        }),
      )
      .then((out) => {
        if (!cancelled) setHtml(out)
      })
      .catch(() => {
        // Leave html null: the plain fallback below is already correct.
      })
    return () => {
      cancelled = true
    }
  }, [code])

  const base = cn(
    "bg-muted/50 mt-1 max-h-64 overflow-auto rounded px-2 py-1 font-mono text-[11px] whitespace-pre",
    "[&_pre]:!bg-transparent [&_pre]:!p-0",
    className,
  )

  if (html) {
    // Shiki output, built from `code` on this same client — not user HTML.
    return <div className={base} dangerouslySetInnerHTML={{ __html: html }} />
  }
  return <div className={base}>{code}</div>
}
