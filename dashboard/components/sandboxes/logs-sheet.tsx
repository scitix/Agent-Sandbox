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

import { useRef, useEffect, useState, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { parseAsString, useQueryState } from "nuqs"
import { ScrollText, Search, X, ChevronUp, ChevronDown, Download } from "lucide-react"
import { Sheet, SheetContent } from "@/components/ui/sheet"
import { SandboxSheetHeader } from "@/components/sandboxes/sandbox-sheet-header"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { sandboxQueryOptions } from "@/lib/queries"
import { basePath, getToken } from "@/lib/api/client"
import { store, impersonationAtom } from "@/lib/atoms"
import { useTranslation } from "@/lib/i18n"
import { useExternalLogsConfigured } from "@/hooks/use-external-logs"
import { useClusterID } from "@/hooks/use-cluster-id"

// ─── nuqs URL param names ──────────────────────────────────────────────────────

export const LOGS_SANDBOX_ID_PARAM = "logsFor"

// ─── Lines selector ───────────────────────────────────────────────────────────

const LINE_OPTIONS = [
  { label: "All", value: 0 },
  { label: "100", value: 100 },
  { label: "500", value: 500 },
  { label: "1000", value: 1000 },
]

// ─── Source type ─────────────────────────────────────────────────────────────

// ─── NDJSON types ─────────────────────────────────────────────────────────────
// Aligned with the external log service format (also used by the AgentBox backend).

interface NdjsonEntry {
  _timestamp?: string
  container_name?: string
  log: string
  pod_name?: string
  namespace_name?: string
  node_name?: string
}

interface NdjsonMeta {
  _meta: true
  source: string
  truncated: boolean
  pod_name?: string
}

type NdjsonLine = NdjsonEntry | NdjsonMeta | { _meta?: never; log?: never; error: string }

// ─── Inner log viewer (mounted only when sheet is open) ────────────────────────

interface LogViewerProps {
  sandboxId: string
  clusterID: string
  /** Ref shared with the parent LogsSheet so it can abort the stream immediately on close. */
  abortRef: React.RefObject<AbortController | null>
}

function LogViewer({ sandboxId, clusterID, abortRef }: LogViewerProps) {
  const { t } = useTranslation()
  const [lines, setLines] = useState(100)
  const [metaInfo, setMetaInfo] = useState<NdjsonMeta | null>(null)
  // isStreaming: true while an NDJSON stream is actively open (no meta line received yet).
  // Used to drive the "live" breathing indicator.
  const [isStreaming, setIsStreaming] = useState(false)
  const [lineCount, setLineCount] = useState(0)

  const isExternalLogsConfigured = useExternalLogsConfigured()

  // ── Search state (always visible) ────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState("")
  // Debounced value actually sent to xterm SearchAddon (300ms, min 4 chars)
  const [debouncedQuery, setDebouncedQuery] = useState("")
  useEffect(() => {
    const id = setTimeout(() => {
      setDebouncedQuery(searchQuery.length >= 4 ? searchQuery : "")
    }, 300)
    return () => clearTimeout(id)
  }, [searchQuery])
  // Match counters fed by SearchAddon.onDidChangeResults.
  // onDidChangeResults only fires when decorations are enabled (which we do).
  // resultIndex is 0-based; -1 means the active match is beyond the highlight limit.
  const [searchResultIndex, setSearchResultIndex] = useState(-1)
  const [searchResultCount, setSearchResultCount] = useState(0)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const searchAddonRef = useRef<import("@xterm/addon-search").SearchAddon | null>(null)

  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<import("@xterm/xterm").Terminal | null>(null)
  const fitAddonRef = useRef<import("@xterm/addon-fit").FitAddon | null>(null)
  const fetchCountRef = useRef(0)
  // Accumulates raw `log` fields from each NDJSON entry so the user can download
  // the logs without a second backend round-trip. xterm's scrollback is capped,
  // so we keep our own full-fidelity buffer here.
  const logBufferRef = useRef<string[]>([])

  // ── Fetch sandbox details ─────────────────────────────────────────────────
  const { data: sandboxEnvelope } = useQuery({
    ...sandboxQueryOptions(sandboxId),
    refetchOnWindowFocus: false,
  })

  const sandbox = sandboxEnvelope?.sandbox
  const sandboxStatus = sandbox?.status ?? ""
  const isTerminated =
    sandboxStatus === "Completed" || sandboxStatus === "Failed" || sandboxStatus === "Canceled" || sandboxStatus === "Released"

  // ── xterm initialization ──────────────────────────────────────────────────
  useEffect(() => {
    if (!containerRef.current) return

    let disposed = false

    void (async () => {
      const { Terminal } = await import("@xterm/xterm")
      const { FitAddon } = await import("@xterm/addon-fit")
      const { WebLinksAddon } = await import("@xterm/addon-web-links")
      const { SearchAddon } = await import("@xterm/addon-search")

      if (disposed || !containerRef.current) return

      const term = new Terminal({
        allowProposedApi: true,
        disableStdin: true,
        cursorBlink: false,
        fontSize: 12,
        fontFamily: '"JetBrains Mono", "Fira Code", "Cascadia Code", monospace',
        scrollback: 50000,
        convertEol: true,
        scrollOnUserInput: false,
        theme: {
          background: "#09090b",
          foreground: "#e4e4e7",
          selectionBackground: "#3f3f46",
          selectionForeground: "#09090b",
        },
      })

      const fitAddon = new FitAddon()
      const searchAddon = new SearchAddon()

      term.loadAddon(fitAddon)
      term.loadAddon(searchAddon)
      term.loadAddon(new WebLinksAddon())

      containerRef.current.innerHTML = ""
      term.open(containerRef.current)

      fitAddon.fit()

      termRef.current = term
      fitAddonRef.current = fitAddon
      searchAddonRef.current = searchAddon

      // Subscribe to match-count updates.
      // onDidChangeResults fires after every findNext/findPrevious when decorations
      // are enabled. resultIndex is 0-based; resultCount is the total hit count.
      searchAddon.onDidChangeResults((e) => {
        setSearchResultIndex(e.resultIndex)
        setSearchResultCount(e.resultCount)
      })

      const ro = new ResizeObserver(() => {
        fitAddon.fit()
      })
      ro.observe(containerRef.current)

      const origDispose = term.dispose.bind(term)
      term.dispose = () => {
        ro.disconnect()
        origDispose()
      }
    })()

    return () => {
      disposed = true
      termRef.current?.dispose()
      termRef.current = null
      fitAddonRef.current = null
      searchAddonRef.current = null
    }
  }, [])

  // ── Search helpers ────────────────────────────────────────────────────────
  const doSearch = useCallback(
    (direction: "next" | "prev") => {
      const addon = searchAddonRef.current
      if (!addon || !debouncedQuery) return
      const opts = {
        regex: false,
        wholeWord: false,
        caseSensitive: false,
        incremental: false,
        decorations: {
          matchBackground: "#854d0e",
          matchBorder: "#ca8a04",
          matchOverviewRuler: "#ca8a04",
          activeMatchBackground: "#f59e0b",
          activeMatchBorder: "#fbbf24",
          activeMatchColorOverviewRuler: "#fbbf24",
        },
      }
      if (direction === "next") {
        addon.findNext(debouncedQuery, opts)
      } else {
        addon.findPrevious(debouncedQuery, opts)
      }
    },
    [debouncedQuery],
  )

  // Re-run search whenever debounced query changes.
  useEffect(() => {
    if (!debouncedQuery) {
      searchAddonRef.current?.clearDecorations()
      setSearchResultIndex(-1)
      setSearchResultCount(0)
      return
    }
    doSearch("next")
  }, [debouncedQuery, doSearch])

  // Keyboard shortcut: Ctrl+F / Cmd+F to focus the search input.
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "f") {
        e.preventDefault()
        searchInputRef.current?.focus()
        searchInputRef.current?.select()
      }
      if (e.key === "Escape" && document.activeElement === searchInputRef.current) {
        setSearchQuery("")
        setDebouncedQuery("")
        searchAddonRef.current?.clearDecorations()
        searchInputRef.current?.blur()
      }
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [])

  // ── Download ──────────────────────────────────────────────────────────────
  const handleDownload = useCallback(() => {
    if (logBufferRef.current.length === 0) return
    // Each entry's `log` field may already end with a newline (typical for
    // container stdout). Trim to ensure exactly one \n between lines.
    const body = logBufferRef.current.map((l) => l.replace(/\r?\n$/, "")).join("\n") + "\n"
    const blob = new Blob([body], { type: "text/plain;charset=utf-8" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `sandbox-${sandboxId}-logs.txt`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }, [sandboxId])

  // ── NDJSON log fetch ──────────────────────────────────────────────────────
  const fetchLogs = useCallback(async () => {
    const term = termRef.current
    if (!term) return

    // Cancel any in-flight request (tab switch, line count change, etc.)
    abortRef.current?.abort()
    const controller = new AbortController()
    // Update the shared ref so the parent LogsSheet can also abort on close.
    // useRef returns a mutable object; the RefObject type in React 19 already
    // allows `.current` writes, so no cast is needed.
    abortRef.current = controller

    const thisFetch = ++fetchCountRef.current

    setIsStreaming(true)
    setMetaInfo(null)
    setLineCount(0)
    term.clear()
    logBufferRef.current = []
    searchAddonRef.current?.clearDecorations()
    setSearchQuery("")
    setDebouncedQuery("")

    // Terminated sandboxes with the external log service available use the BFF
    // sandbox-logs endpoint instead of the live K8s stream.
    const useExternalLogs = isTerminated && isExternalLogsConfigured

    if (isTerminated && !useExternalLogs) {
      return
    }

    const linesParam = lines > 0 ? `?lines=${lines}` : ""

    try {
      const impersonation = store.get(impersonationAtom)
      const headers: Record<string, string> = { Authorization: `Bearer ${getToken()}` }
      if (impersonation?.team && impersonation?.user) {
        headers["X-Impersonate-Team"] = impersonation.team
        headers["X-Impersonate-User"] = impersonation.user
      }

      let url: string
      let fetchInit: RequestInit
      if (useExternalLogs) {
        url = `${basePath}/api/sandbox-logs`
        fetchInit = {
          method: "POST",
          headers: { ...headers, "Content-Type": "application/json" },
          body: JSON.stringify({ sandbox, clusterID }),
          signal: controller.signal,
        }
      } else {
        url = `${basePath}/api/clusters/${clusterID}/v1/sandboxes/${sandboxId}/logs/stream${linesParam}`
        fetchInit = { headers, signal: controller.signal }
      }

      const res = await fetch(url, fetchInit)
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      }

      const reader = res.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ""
      let count = 0

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        if (thisFetch !== fetchCountRef.current) break

        buffer += decoder.decode(value, { stream: true })
        const chunks = buffer.split("\n")
        buffer = chunks.pop()!

        for (const chunk of chunks) {
          const trimmed = chunk.trim()
          if (!trimmed) continue
          try {
            const obj = JSON.parse(trimmed) as NdjsonLine
            if ("_meta" in obj && obj._meta) {
              setMetaInfo(obj as NdjsonMeta)
              setIsStreaming(false)
            } else if ("log" in obj && obj.log !== undefined) {
              const entry = obj as NdjsonEntry
              term.write(formatEntry(entry) + "\r\n")
              logBufferRef.current.push(entry.log)
              count++
              setLineCount(count)
            }
          } catch {
            // malformed JSON line — skip
          }
        }
      }

      const trimmed = buffer.trim()
      if (trimmed) {
        try {
          const obj = JSON.parse(trimmed) as NdjsonLine
          if ("_meta" in obj && obj._meta) {
            setMetaInfo(obj as NdjsonMeta)
            setIsStreaming(false)
          } else if ("log" in obj && obj.log !== undefined) {
            const entry = obj as NdjsonEntry
            term.write(formatEntry(entry) + "\r\n")
            logBufferRef.current.push(entry.log)
            setLineCount((c) => c + 1)
          }
        } catch {
          // ignore
        }
      }
    } catch (err) {
      if ((err as Error).name === "AbortError") return
    } finally {
      if (thisFetch === fetchCountRef.current) {
        setIsStreaming(false)
      }
    }
  }, [abortRef, lines, clusterID, sandboxId, isTerminated, isExternalLogsConfigured, sandbox])

  function formatEntry(e: NdjsonEntry): string {
    const parts: string[] = []
    if (e._timestamp) {
      const ts = new Date(e._timestamp).toISOString().replace("T", " ").replace("Z", "")
      parts.push(`\x1b[2m${ts}\x1b[0m`)
    }
    if (e.container_name) {
      parts.push(`\x1b[36m[${e.container_name}]\x1b[0m`)
    }
    parts.push(e.log)
    return parts.join(" ")
  }

  const [xtermReady, setXtermReady] = useState(false)
  useEffect(() => {
    const interval = setInterval(() => {
      if (termRef.current) {
        setXtermReady(true)
        clearInterval(interval)
      }
    }, 50)
    return () => clearInterval(interval)
  }, [])

  useEffect(() => {
    if (!xtermReady) return
    void fetchLogs()
    return () => {
      abortRef.current?.abort()
    }
  }, [xtermReady, fetchLogs, abortRef])

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* ── Combined toolbar: lines selector + status + search ── */}
      <div className="border-border flex h-[33px] shrink-0 items-center gap-2 border-b px-3">
        {/* Lines selector (hidden for external logs — always returns full history) */}
        {!(isTerminated && isExternalLogsConfigured) && (
          <>
            <Select
              value={String(lines)}
              onValueChange={(val) => {
                if (val !== undefined) setLines(Number(val))
              }}
            >
              <SelectTrigger
                size="sm"
                className="h-6 gap-1 border-0 bg-transparent px-1.5 font-mono text-xs uppercase shadow-none focus-visible:ring-0"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="start">
                {LINE_OPTIONS.map((opt) => (
                  <SelectItem
                    key={opt.value}
                    value={String(opt.value)}
                    className="font-mono text-xs uppercase"
                  >
                    {opt.value === 0 ? t("sandboxes.all") : opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Divider */}
            <div className="bg-border h-4 w-px shrink-0" />
          </>
        )}

        {/* Inline search (always visible) */}
        <div className="flex min-w-0 flex-1 items-center gap-1">
          <Search className="text-muted-foreground/60 h-3 w-3 shrink-0" />
          <input
            ref={searchInputRef}
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                if (e.shiftKey) {
                  doSearch("prev")
                } else {
                  doSearch("next")
                }
              }
              if (e.key === "Escape") {
                setSearchQuery("")
                setDebouncedQuery("")
                searchAddonRef.current?.clearDecorations()
                searchInputRef.current?.blur()
              }
            }}
            placeholder={t("sandboxes.searchPlaceholder")}
            className="text-foreground placeholder:text-muted-foreground/40 min-w-0 flex-1 bg-transparent font-mono text-xs outline-none"
          />
          {/* Hint: need at least 4 chars */}
          {searchQuery.length > 0 && searchQuery.length < 4 && (
            <span className="text-muted-foreground/50 shrink-0 font-mono text-xs">
              {t("sandboxes.searchMinChars")}
            </span>
          )}
          {/* Match counter — shown when debounced query is active */}
          {debouncedQuery && searchResultCount > 0 && (
            <span className="text-muted-foreground/60 shrink-0 font-mono text-xs tabular-nums">
              {searchResultIndex === -1 ? "?" : searchResultIndex + 1}
              {" / "}
              {searchResultCount}
            </span>
          )}
          {debouncedQuery && searchResultCount === 0 && (
            <span className="shrink-0 font-mono text-xs text-red-400/70">
              {t("sandboxes.noMatch")}
            </span>
          )}
          {searchQuery && (
            <>
              <button
                onClick={() => doSearch("prev")}
                className="text-muted-foreground hover:text-foreground shrink-0 transition-colors"
                title="Previous match (Shift+Enter)"
              >
                <ChevronUp className="h-3 w-3" />
              </button>
              <button
                onClick={() => doSearch("next")}
                className="text-muted-foreground hover:text-foreground shrink-0 transition-colors"
                title="Next match (Enter)"
              >
                <ChevronDown className="h-3 w-3" />
              </button>
              <button
                onClick={() => {
                  setSearchQuery("")
                  setDebouncedQuery("")
                  searchAddonRef.current?.clearDecorations()
                }}
                className="text-muted-foreground hover:text-foreground shrink-0 transition-colors"
                title="Clear search (Esc)"
              >
                <X className="h-3 w-3" />
              </button>
            </>
          )}
        </div>

        {/* Divider */}
        <div className="bg-border h-4 w-px shrink-0" />

        {/* Status area: line count + meta badges + live indicator */}
        <div className="flex shrink-0 items-center gap-1.5">
          <span className="text-muted-foreground/50 font-mono text-xs">{lineCount} lines</span>
          {metaInfo?.source && (
            <Badge
              variant="outline"
              className={`font-mono text-xs uppercase ${metaInfo.source === "live"
                ? "border-green-500/30 bg-green-500/10 text-green-400"
                : metaInfo.source === "runtime"
                  ? "border-blue-500/30 bg-blue-500/10 text-blue-400"
                  : metaInfo.source === "external-logs"
                    ? "border-purple-500/30 bg-purple-500/10 text-purple-400"
                    : "border-yellow-500/30 bg-yellow-500/10 text-yellow-400"
                }`}
            >
              {metaInfo.source === "external-logs" ? t("sandboxes.externalLogsSource") : metaInfo.source}
            </Badge>
          )}

          {metaInfo?.truncated && (
            <Badge
              variant="outline"
              className="border-orange-500/30 bg-orange-500/10 font-mono text-xs text-orange-400 uppercase"
            >
              {t("sandboxes.truncated")}
            </Badge>
          )}

          {/* Download button — uses the in-memory buffer, no backend call */}
          <button
            onClick={handleDownload}
            disabled={isStreaming || lineCount === 0}
            className="text-muted-foreground hover:text-foreground shrink-0 transition-colors disabled:cursor-not-allowed disabled:opacity-40"
            title={t("sandboxes.downloadLogs")}
          >
            <Download className="h-3 w-3" />
          </button>

          {/* Live / breathing indicator — replaces the refresh button.
              Pulses while the stream is open; goes solid green once complete. */}
          <span
            title={isStreaming ? t("sandboxes.streamActive") : t("sandboxes.streamComplete")}
            className={`inline-flex h-2 w-2 shrink-0 rounded-full ${isStreaming
              ? "animate-pulse bg-green-400"
              : metaInfo
                ? "bg-green-600/60"
                : "bg-muted-foreground/30"
              }`}
          />
        </div>
      </div>

      {/* Log body */}
      <div className="relative min-h-0 flex-1 bg-black p-2">
        <div ref={containerRef} className="h-full w-full bg-zinc-950" />
      </div>
    </div>
  )
}

// ─── Public LogsSheet component ───────────────────────────────────────────────

/**
 * LogsSheet — URL-bound Sheet that shows sandbox logs.
 *
 * The `logsFor` URL query param drives the open state:
 *   - absent / empty  → sheet closed
 *   - <sandboxId>     → sheet open, fetching logs for that sandbox
 */
export function LogsSheet() {
  const { t } = useTranslation()
  const clusterID = useClusterID()

  // Shared abort ref: hoisted here so handleOpenChange can immediately cancel
  // the in-flight stream when the sheet is closed, without waiting for the
  // LogViewer component to go through its React unmount cycle.
  const abortRef = useRef<AbortController | null>(null)

  const [sandboxId, setSandboxId] = useQueryState(
    LOGS_SANDBOX_ID_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )

  const isOpen = !!sandboxId

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      // Immediately abort the stream — don't wait for LogViewer to unmount.
      // This is the fastest path to cancelling the backend tail -f / K8s log stream.
      abortRef.current?.abort()
      void setSandboxId(null)
    }
  }

  return (
    <Sheet open={isOpen} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl data-[side=right]:sm:max-w-5xl"
      >
        <SandboxSheetHeader icon={ScrollText} title={t("sandboxes.logs")} sandboxId={sandboxId} />

        {isOpen && sandboxId && (
          <LogViewer sandboxId={sandboxId} clusterID={clusterID} abortRef={abortRef} />
        )}
      </SheetContent>
    </Sheet>
  )
}
