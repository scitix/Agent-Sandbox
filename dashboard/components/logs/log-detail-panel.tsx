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

// The detail panel under the log list: everything we can say about ONE line.
//
// Three jobs, in the order an operator needs them:
//  1. when it was collected — plus a re-query that reopens the window around it
//     (the "show me ±X seconds without the keyword" move);
//  2. what it says — the parse engine's structure, with embedded JSON unfolded;
//  3. where it leads — a jump into the observability platform when the line
//     carries a trace/span.
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { JsonBlock } from "@/components/logs/json-block"
import { useCopyToClipboardWithText } from "@/hooks/use-copy-to-clipboard"
import { formatLocalTimestamp } from "@/lib/utils/format-time"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { CopyIcon, SearchIcon, X } from "lucide-react"
import { useMemo, useState } from "react"

import type { LogEntry } from "@/components/logs/types"

import { type LogLevel, parseLogLine } from "@/components/logs/log-parse"

/** The window + keyword a re-query should run with (epoch ms). */
export interface LogRequery {
  from: number
  to: number
  /** Empty string clears the server-side keyword. */
  match: string
}

/** Context about the log source, shown to the agent in an Ask AI attachment. */
// Default context window around the selected line. Deliberately tight: the point
// of the re-query is to NARROW an over-broad search down to what happened right
// around this line, and without the keyword even two seconds of a busy scheduler
// is a lot of lines.
const DEFAULT_CONTEXT_SECONDS = 2

// Same per-theme shades as the list's severity colors (log-viewer.tsx), so a line
// reads identically in both places.
const LEVEL_CLASS: Record<LogLevel, string> = {
  info: "text-sky-700 dark:text-sky-400",
  warning: "text-amber-700 dark:text-amber-400",
  error: "text-red-700 dark:text-red-400",
  fatal: "text-red-800 dark:text-red-300",
}

export function LogDetailPanel({
  entry,
  currentMatch,
  onClose,
  onRequery,
}: {
  entry: LogEntry
  currentMatch?: string
  onClose: () => void
  /** Absent when the host page can't re-query; the form is then hidden. */
  onRequery?: (next: LogRequery, keepHighlight: boolean) => void
}) {
  const { t } = useTranslation()
  const { handleCopyWithText } = useCopyToClipboardWithText()

  const parsed = useMemo(() => parseLogLine(entry.log), [entry.log])
  // Opening this panel must not put a request on the wire. The only thing here
  // No trace link here: AgentBox has no distributed-tracing backend to point
  // one at. The parser still extracts a trace id when a line carries one, and
  // it is shown as a copyable field like any other.

  const centerMs = entry.timestamp ? Date.parse(entry.timestamp) : Number.NaN
  const canRequery = !!onRequery && Number.isFinite(centerMs)

  const [before, setBefore] = useState(DEFAULT_CONTEXT_SECONDS)
  const [after, setAfter] = useState(DEFAULT_CONTEXT_SECONDS)
  const [keepKeyword, setKeepKeyword] = useState(false)
  const [keepHighlight, setKeepHighlight] = useState(true)

  const runRequery = () => {
    if (!onRequery || !Number.isFinite(centerMs)) return
    onRequery(
      {
        from: centerMs - Math.max(0, before) * 1000,
        to: centerMs + Math.max(0, after) * 1000,
        match: keepKeyword ? (currentMatch ?? "") : "",
      },
      keepHighlight,
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Header: identity on the left, actions on the right. */}
      <div className="border-border flex h-9 shrink-0 items-center gap-2 border-b px-2 font-mono text-xs">
        {parsed.level && (
          <span className={cn("shrink-0 uppercase", LEVEL_CLASS[parsed.level])}>
            {parsed.level}
          </span>
        )}
        {parsed.source && <span className="text-muted-foreground shrink-0">{parsed.source}</span>}
        <span className="text-muted-foreground/80 truncate">
          {entry.containerName ?? entry.podName ?? ""}
        </span>
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            title={t("componentLogs.detail.copyRaw")}
            onClick={() => handleCopyWithText(entry.log)}
          >
            <CopyIcon className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            title={t("componentLogs.detail.close")}
            onClick={onClose}
          >
            <X className="size-3.5" />
          </Button>
        </div>
      </div>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 py-2 text-xs">
        {/* ── times + re-query ── */}
        <section className="space-y-2">
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
            <Field label={t("componentLogs.detail.ingestTime")}>
              {entry.timestamp ? formatLocalTimestamp(entry.timestamp, { withMillis: true }) : "—"}
            </Field>
            {parsed.inlineTime && (
              <Field label={t("componentLogs.detail.processClock")}>{parsed.inlineTime}</Field>
            )}
            {parsed.threadId && (
              <Field label={t("componentLogs.detail.thread")}>{parsed.threadId}</Field>
            )}
          </div>

          {onRequery && (
            <div className="bg-muted/40 flex flex-wrap items-end gap-3 rounded-md p-2">
              <div className="flex flex-col gap-1">
                <Label className="text-muted-foreground text-[11px]">
                  {t("componentLogs.detail.beforeSeconds")}
                </Label>
                <Input
                  type="number"
                  min={0}
                  value={before}
                  onChange={(e) => setBefore(Number(e.target.value))}
                  className="h-7 w-20 font-mono text-xs"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label className="text-muted-foreground text-[11px]">
                  {t("componentLogs.detail.afterSeconds")}
                </Label>
                <Input
                  type="number"
                  min={0}
                  value={after}
                  onChange={(e) => setAfter(Number(e.target.value))}
                  className="h-7 w-20 font-mono text-xs"
                />
              </div>
              <label className="flex items-center gap-1.5 pb-1.5">
                <input
                  type="checkbox"
                  checked={keepKeyword}
                  onChange={(e) => setKeepKeyword(e.target.checked)}
                />
                {t("componentLogs.detail.keepKeyword")}
              </label>
              <label className="flex items-center gap-1.5 pb-1.5">
                <input
                  type="checkbox"
                  checked={keepHighlight}
                  onChange={(e) => setKeepHighlight(e.target.checked)}
                />
                {t("componentLogs.detail.keepHighlight")}
              </label>
              <Button
                size="sm"
                variant="outline"
                className="h-7"
                disabled={!canRequery}
                onClick={runRequery}
                title={canRequery ? undefined : t("componentLogs.detail.noIngestTime")}
              >
                <SearchIcon className="size-3.5" />
                {t("componentLogs.detail.requery")}
              </Button>
            </div>
          )}
        </section>

        {/* ── parsed content ── */}
        {parsed.message && (
          <section className="space-y-1">
            <SectionTitle>{t("componentLogs.detail.message")}</SectionTitle>
            <p className="font-mono break-words whitespace-pre-wrap">{parsed.message}</p>
          </section>
        )}

        {parsed.fields.length > 0 && (
          <section className="space-y-1">
            <SectionTitle>{t("componentLogs.detail.fields")}</SectionTitle>
            <div className="divide-border divide-y">
              {parsed.fields.map((field) => (
                <div key={field.key} className="group py-1.5">
                  <div className="flex items-center gap-2">
                    <span className="text-muted-foreground font-mono">{field.key}</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-5 opacity-0 transition-opacity group-hover:opacity-100"
                      title={t("componentLogs.detail.copyValue")}
                      onClick={() => handleCopyWithText(field.json ?? field.value)}
                    >
                      <CopyIcon className="size-3" />
                    </Button>
                  </div>
                  {field.json ? (
                    <JsonBlock
                      code={field.json}
                      className="bg-muted/50 mt-1 max-h-64 text-[11px]"
                    />
                  ) : (
                    <p className="font-mono break-all">{field.value}</p>
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {/* The raw line is always last and verbatim: the parse above is
            best-effort, so the source of truth has to stay reachable. */}
        <section className="space-y-1">
          <SectionTitle>{t("componentLogs.detail.raw")}</SectionTitle>
          <pre className="bg-muted/50 overflow-x-auto rounded-md p-2 font-mono text-[11px] break-all whitespace-pre-wrap">
            {entry.log}
          </pre>
        </section>
      </div>
    </div>
  )
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <h4 className="text-muted-foreground text-[11px] font-medium uppercase">{children}</h4>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <span className="flex items-baseline gap-1.5">
      <span className="text-muted-foreground text-[11px]">{label}</span>
      <span className="font-mono">{children}</span>
    </span>
  )
}
