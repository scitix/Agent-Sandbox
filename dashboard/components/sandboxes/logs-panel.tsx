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
 * A sandbox's logs.
 *
 * Two modes, because a live sandbox and a finished one are answered by
 * different systems and support different questions:
 *
 *  • Running — the Pod still exists, so lines stream from the Kubernetes log
 *    API as they are written. There is no window to choose (the stream is
 *    always "from now backwards N lines") and no server-side search to run,
 *    so the sidebar is not shown; the viewer's own highlight covers what is
 *    on screen.
 *
 *  • Finished — the Pod is recycled and the only remaining copy is in the
 *    central log service, which is a query rather than a stream. That makes a
 *    window and a keyword meaningful, so the sidebar appears, defaulted to
 *    when the sandbox actually ran: startedAt → terminatedAt. NOT claimedAt,
 *    which includes the arming that precedes the user's first command, and
 *    not "now", which would be the recycle time and mostly empty.
 *
 * The row list, the selection detail and the resizable split all come from
 * `components/logs`.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Loader2, RefreshCw, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { LogViewer } from "@/components/logs/log-viewer"
import type { LogEntry } from "@/components/logs/types"
import { useClusterID } from "@/hooks/use-cluster-id"
import { basePath, getToken } from "@/lib/api/client"
import { store, impersonationAtom } from "@/lib/atoms"
import { useTranslation } from "@/lib/i18n"
import { sandboxLogsConfigQueryOptions, sandboxQueryOptions } from "@/lib/queries"

/**
 * The sandbox's own container, and the default everywhere. A Pod also runs the
 * egress proxy and the injector init containers; the proxy logs a line per
 * outbound connection it evaluates, which buries what the sandbox printed.
 */
const SANDBOX_CONTAINER = "sandbox"

/** Line cap for a finished sandbox's query. Matches the viewer's wrap limit. */
const DEFAULT_LIMIT = 1000

const LINE_OPTIONS = [0, 100, 500, 1000]

/** One NDJSON line, as both the live stream and the central-log BFF emit it. */
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
type NdjsonLine = NdjsonEntry | NdjsonMeta | { error: string }

function isMeta(l: NdjsonLine): l is NdjsonMeta {
  return "_meta" in l && l._meta === true
}
function isEntry(l: NdjsonLine): l is NdjsonEntry {
  return "log" in l && typeof (l as NdjsonEntry).log === "string"
}

/** Local-time `YYYY-MM-DDTHH:mm` for a datetime-local input. */
function toLocalInput(iso: string | undefined): string {
  if (!iso) return ""
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ""
  const pad = (n: number) => String(n).padStart(2, "0")
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  )
}

export function SandboxLogsPanel({ sandboxId }: { sandboxId: string }) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const abortRef = useRef<AbortController | null>(null)

  const { data: envelope } = useQuery({
    ...sandboxQueryOptions(sandboxId),
    refetchOnWindowFocus: false,
  })
  const sandbox = envelope?.sandbox
  const status = sandbox?.status ?? ""
  const isTerminated =
    status === "Completed" || status === "Failed" || status === "Canceled" || status === "Released"

  const { data: logsConfig } = useQuery(sandboxLogsConfigQueryOptions())
  const useCentral = isTerminated && (logsConfig?.configured ?? false)
  // Both of these arrive asynchronously, and each one flipping restarts the
  // fetch. Waiting until they have both landed is what makes the view load
  // once: without it the first two runs are thrown away, and the discarded
  // third-of-a-second still clears a line the user may already have clicked.
  const ready = !!sandbox && logsConfig !== undefined

  // ── Container ────────────────────────────────────────────────────────────
  const containers = useMemo(() => {
    const names = Object.keys(sandbox?.containerImages ?? {})
    if (names.length === 0) return [SANDBOX_CONTAINER]
    const rest = names.filter((n) => n !== SANDBOX_CONTAINER).sort()
    return names.includes(SANDBOX_CONTAINER) ? [SANDBOX_CONTAINER, ...rest] : rest
  }, [sandbox?.containerImages])
  const [container, setContainer] = useState("")

  // ── Window + keyword, for the finished-sandbox query only ────────────────
  //
  // Defaulted to the run itself, and only once the record has loaded: seeding
  // from an undefined startedAt would pin an empty window that the user then
  // has to notice and fix.
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [keyword, setKeyword] = useState("")
  const seeded = useRef(false)
  // Seeding the form from data that arrives asynchronously: the record is not
  // available on first render, so there is nowhere earlier to do this. Guarded
  // by a ref so it runs once and never overwrites what the user typed.

  useEffect(() => {
    if (seeded.current || !sandbox) return
    const start = sandbox.startedAt ?? sandbox.claimedAt
    if (!start) return
    seeded.current = true
    // eslint-disable-next-line react-hooks/set-state-in-effect -- seeds the form from data that only arrives asynchronously; the ref makes it run once
    setFrom(toLocalInput(start))
    setTo(toLocalInput(sandbox.terminatedAt))
  }, [sandbox])

  const [limit, setLimit] = useState(DEFAULT_LIMIT)
  const [lines, setLines] = useState(100)

  // ── Fetch ────────────────────────────────────────────────────────────────
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [truncated, setTruncated] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // The keyword actually in effect, as opposed to the one being typed. The
  // viewer shows it as "keep keyword" on a re-query, so it must not change
  // under the user's cursor.
  const [appliedKeyword, setAppliedKeyword] = useState("")

  // The request depends on the sandbox's IDENTITY, not on the object: react-query
  // hands back a new reference on every background refetch, and depending on the
  // object re-ran the whole fetch each time — clearing the view, and with it any
  // selected line, while nothing had actually changed. The central-log call needs
  // the whole record, so it is snapshotted into the memo alongside the fields the
  // query is actually keyed on.
  const podName = sandbox?.podName ?? ""
  const claimedAt = sandbox?.claimedAt ?? ""
  const startedAt = sandbox?.startedAt ?? ""
  const terminatedAt = sandbox?.terminatedAt ?? ""
  const queryBody = useMemo(
    () => sandbox,
    // eslint-disable-next-line react-hooks/exhaustive-deps -- re-snapshot only when the identity below changes, not on every refetch
    [podName, claimedAt, startedAt, terminatedAt],
  )

  const fetchLogs = useCallback(async () => {
    const sandbox = queryBody
    if (!sandbox || !ready) return
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setIsLoading(true)
    setError(null)
    setEntries([])
    setTruncated(false)

    const headers: Record<string, string> = { Authorization: `Bearer ${getToken()}` }
    const impersonation = store.get(impersonationAtom)
    if (impersonation?.team && impersonation?.user) {
      headers["X-Impersonate-Team"] = impersonation.team
      headers["X-Impersonate-User"] = impersonation.user
    }

    let url: string
    let init: RequestInit
    if (useCentral) {
      url = `${basePath}/api/sandbox-logs`
      init = {
        method: "POST",
        headers: { ...headers, "Content-Type": "application/json" },
        body: JSON.stringify({
          sandbox,
          clusterID,
          container: container || undefined,
          from: from ? new Date(from).toISOString() : undefined,
          to: to ? new Date(to).toISOString() : undefined,
          keyword: keyword || undefined,
          limit,
        }),
        signal: controller.signal,
      }
      setAppliedKeyword(keyword)
    } else {
      const qs = new URLSearchParams()
      if (lines > 0) qs.set("lines", String(lines))
      if (container) qs.set("container", container)
      url = `${basePath}/api/clusters/${clusterID}/v1/sandboxes/${sandboxId}/logs/stream${
        qs.size > 0 ? `?${qs}` : ""
      }`
      init = { headers, signal: controller.signal }
      setAppliedKeyword("")
    }

    try {
      const res = await fetch(url, init)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const body = res.body
      if (!body) throw new Error("empty response")

      // A finished sandbox is a query, not a stream: the whole result is read
      // before anything is rendered. Batching it in would only make the view
      // flicker through partial states on its way to the same place — and it
      // keeps isLoading true while it does, which hides the detail panel.
      if (useCentral) {
        const text = await new Response(body).text()
        const all: LogEntry[] = []
        for (const raw of text.split("\n")) {
          const line = raw.trim()
          if (!line) continue
          let parsed: NdjsonLine
          try {
            parsed = JSON.parse(line) as NdjsonLine
          } catch {
            continue
          }
          if (isMeta(parsed)) {
            setTruncated(parsed.truncated)
            continue
          }
          if ("error" in parsed) {
            setError(parsed.error)
            continue
          }
          if (!isEntry(parsed)) continue
          all.push({
            timestamp: parsed._timestamp ?? "",
            log: parsed.log,
            containerName: parsed.container_name,
            podName: parsed.pod_name,
            namespaceName: parsed.namespace_name,
            nodeName: parsed.node_name,
          })
        }
        setEntries(all)
        return
      }

      // Append in batches rather than per line: a busy sandbox emits faster
      // than React can commit, and one setState per line makes the view lag
      // behind the stream it is meant to show.
      const reader = body.getReader()
      const decoder = new TextDecoder()
      let buffer = ""
      let pending: LogEntry[] = []
      const flush = () => {
        if (pending.length === 0) return
        const batch = pending
        pending = []
        setEntries((prev) => prev.concat(batch))
      }
      const timer = setInterval(flush, 150)
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const parts = buffer.split("\n")
          buffer = parts.pop() ?? ""
          for (const raw of parts) {
            const line = raw.trim()
            if (!line) continue
            let parsed: NdjsonLine
            try {
              parsed = JSON.parse(line) as NdjsonLine
            } catch {
              continue
            }
            if (isMeta(parsed)) {
              setTruncated(parsed.truncated)
              continue
            }
            if ("error" in parsed) {
              setError(parsed.error)
              continue
            }
            if (!isEntry(parsed)) continue
            // The first line ends the loading state. A live stream stays open
            // for the life of the sandbox, so waiting for it to finish would
            // leave the view saying "Loading logs…" over lines it is already
            // showing — and the detail panel hidden behind it.
            setIsLoading(false)
            pending.push({
              timestamp: parsed._timestamp ?? "",
              log: parsed.log,
              containerName: parsed.container_name,
              podName: parsed.pod_name,
              namespaceName: parsed.namespace_name,
              nodeName: parsed.node_name,
            })
          }
        }
      } finally {
        clearInterval(timer)
        flush()
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError") setError((e as Error).message)
    } finally {
      setIsLoading(false)
    }
  }, [
    ready,
    queryBody,
    clusterID,
    sandboxId,
    container,
    useCentral,
    from,
    to,
    keyword,
    limit,
    lines,
  ])

  // Fetch on mount and whenever the query changes. fetchLogs clears the view
  // synchronously before awaiting the network — the documented "fetch in an
  // effect" pattern, which this rule over-flags.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch-in-an-effect: fetchLogs clears the view before awaiting the network
    void fetchLogs()
    return () => abortRef.current?.abort()
  }, [fetchLogs])

  const toolbar = (
    <div className="border-border flex h-[33px] shrink-0 items-center gap-2 border-b px-3">
      {containers.length > 1 && (
        <Select
          value={container || SANDBOX_CONTAINER}
          onValueChange={(v) => {
            if (v) setContainer(v === SANDBOX_CONTAINER ? "" : v)
          }}
        >
          <SelectTrigger
            size="sm"
            className="h-6 gap-1 border-0 bg-transparent px-1.5 font-mono text-xs shadow-none focus-visible:ring-0"
            aria-label={t("sandboxes.logs.container")}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="start">
            {containers.map((n) => (
              <SelectItem key={n} value={n} className="font-mono text-xs">
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {!useCentral && (
        <Select
          value={String(lines)}
          onValueChange={(v) => {
            if (v) setLines(Number(v))
          }}
        >
          <SelectTrigger
            size="sm"
            className="h-6 gap-1 border-0 bg-transparent px-1.5 font-mono text-xs uppercase shadow-none focus-visible:ring-0"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="start">
            {LINE_OPTIONS.map((n) => (
              <SelectItem key={n} value={String(n)} className="font-mono text-xs uppercase">
                {n === 0 ? t("sandboxes.all") : String(n)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <span className="text-muted-foreground/60 ml-auto shrink-0 font-mono text-xs">
        {entries.length} {t("componentLogs.lines")}
      </span>
      {error && <span className="text-destructive shrink-0 truncate text-xs">{error}</span>}
    </div>
  )

  const viewer = (
    <div className="flex min-h-0 flex-1 flex-col">
      {toolbar}
      <LogViewer
        entries={entries}
        isLoading={isLoading}
        showTimestamp
        wrap
        truncated={truncated}
        currentMatch={appliedKeyword || undefined}
      />
    </div>
  )

  // A live sandbox has nothing to filter server-side: no sidebar.
  if (!useCentral) return viewer

  return (
    <ResizablePanelGroup orientation="horizontal" className="h-full">
      <ResizablePanel id="log-filters" defaultSize="18%" minSize="14%" maxSize="32%">
        <form
          className="flex h-full flex-col gap-3 overflow-y-auto p-3"
          onSubmit={(e) => {
            e.preventDefault()
            void fetchLogs()
          }}
        >
          <div className="space-y-1">
            <Label htmlFor="log-from" className="text-xs">
              {t("sandboxes.logs.from")}
            </Label>
            <Input
              id="log-from"
              type="datetime-local"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              className="h-7 font-mono text-xs"
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="log-to" className="text-xs">
              {t("sandboxes.logs.to")}
            </Label>
            <Input
              id="log-to"
              type="datetime-local"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              className="h-7 font-mono text-xs"
            />
          </div>
          <p className="text-muted-foreground text-[11px] leading-snug">
            {t("sandboxes.logs.windowHint")}
          </p>
          <div className="space-y-1">
            <Label htmlFor="log-keyword" className="text-xs">
              {t("sandboxes.logs.keyword")}
            </Label>
            <div className="relative">
              <Search className="text-muted-foreground absolute top-1/2 left-2 size-3 -translate-y-1/2" />
              <Input
                id="log-keyword"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                className="h-7 pl-7 font-mono text-xs"
              />
            </div>
          </div>
          <div className="space-y-1">
            <Label htmlFor="log-limit" className="text-xs">
              {t("sandboxes.logs.limit")}
            </Label>
            <Input
              id="log-limit"
              type="number"
              min={1}
              max={10000}
              value={limit}
              onChange={(e) => setLimit(Number(e.target.value) || DEFAULT_LIMIT)}
              className="h-7 font-mono text-xs"
            />
          </div>
          <Button type="submit" size="sm" className="mt-1 h-7" disabled={isLoading}>
            {isLoading ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            {t("sandboxes.logs.search")}
          </Button>
        </form>
      </ResizablePanel>
      <ResizableHandle withHandle />
      <ResizablePanel id="log-body" defaultSize="82%" minSize="40%">
        {viewer}
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}
