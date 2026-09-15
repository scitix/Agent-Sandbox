// The log viewer: a DOM list of log lines plus a detail panel for the selected
// one. Replaces the previous xterm-based terminal.
//
// Why DOM and not a terminal: every interaction this view needs is per-line —
// click to select, hover actions, a highlighted row, keyboard paging, an inline
// detail panel. In a canvas terminal each of those has to be reconstructed from
// hit-testing and marker bookkeeping (and wrapping makes "a line" ambiguous),
// while none of the terminal's actual semantics (stdin, cursor, alt buffer) are
// used here.
//
// Two row layouts:
//  • unwrapped — one line per entry at exactly ROW_HEIGHT, absolutely positioned
//    and windowed. Exact, dense, unbounded: 20 000 lines cost the same as 100.
//    The long tail of a line lives on the horizontal scrollbar.
//  • wrapped (default) — a long line breaks onto several display lines, so nothing
//    hides off-screen. NOT windowed: every row is in the DOM, which is why it is
//    capped at WRAP_MAX_LINES.
//
// Why wrapping cannot be windowed here: windowing needs each row's height before
// it is rendered, and a wrapped row's height is only knowable by laying it out.
// Estimating it arithmetically (monospace cell width × character count) looks
// exact but is not — `pre-wrap` breaks at spaces in preference to filling a line,
// so any space-rich log line takes MORE display lines than the arithmetic says.
// Under-estimated offsets put the rows above the scrolled viewport, i.e. a blank
// screen. Measuring every row instead would work, but at 20 000 rows that is a
// layout pass per row; a line cap is the honest trade.
import { Button } from "@/components/ui/button"
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable"
import { useCopyToClipboardWithText } from "@/hooks/use-copy-to-clipboard"
import { formatLocalTimestamp } from "@/lib/utils/format-time"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import {
  ChevronDown,
  ChevronUp,
  CopyIcon,
  DownloadIcon,
  SearchIcon,
  WrapTextIcon,
  X,
} from "lucide-react"
import { parseAsString, useQueryState } from "nuqs"
import { type ReactNode, memo, useCallback, useEffect, useMemo, useRef, useState } from "react"

import type { LogEntry } from "@/components/logs/types"

import { LogDetailPanel, type LogRequery } from "@/components/logs/log-detail-panel"
import { buildLineIndex, findLineByKey, logLineKey } from "@/components/logs/log-line-key"
import {
  type LogLevel,
  type TokenKind,
  ansiSpans,
  tokenizeLogLine,
} from "@/components/logs/log-parse"

// Row geometry. Must stay in sync with the row's font-size/line-height classes:
// the virtualizer positions rows arithmetically, so a mismatch shows up as drift.
const ROW_HEIGHT = 18
// Pre-rendered runway above and below the viewport. Generous on purpose: a scroll
// repaints faster than React can re-run this component, so the overscan is what
// the user sees during the gap — too small and a fast drag flashes blank bands.
const OVERSCAN_PX = 45 * ROW_HEIGHT
// Upper bound on collected search matches — a 1-char query over 5000 long lines
// would otherwise build a huge index for a count nobody reads.
const MAX_MATCHES = 5000
// Width of the ingest-timestamp prefix in characters ("2026-07-29 12:13:08.156 ").
const TS_PREFIX_CHARS = 25

/**
 * Line count above which wrapping is refused. Wrapped rows are all in the DOM
 * (see the note at the top of this file), so this bounds how much layout a single
 * view can ask for. Callers use it to disable their wrap toggle and say why —
 * silently ignoring the setting would read as a broken switch.
 */
export const WRAP_MAX_LINES = 1000

export function LogViewer({
  entries,
  isLoading,
  showTimestamp,
  wrap,
  onWrapChange,
  leading,
  truncated,
  currentMatch,
  onRequery,
  onPinTimeRange,
}: {
  entries: LogEntry[]
  isLoading: boolean
  showTimestamp: boolean
  /**
   * Wrap long lines onto several display lines instead of scrolling sideways.
   * Honored only up to WRAP_MAX_LINES — past that this view renders unwrapped
   * whatever the setting says (see the note at the top of this file).
   */
  wrap: boolean
  /**
   * Makes `wrap` a control the reader can flip. Omit to keep it fixed at
   * whatever the page passed.
   */
  onWrapChange?: (wrap: boolean) => void
  /**
   * Rendered at the head of this view's toolbar, before the search box.
   *
   * Exists so a page with its own controls (a container picker, a tail length)
   * folds them into this row rather than stacking a second bar above it — two
   * bars meant two line counts, and the reader had to work out that they were
   * the same number.
   */
  leading?: ReactNode
  /** Whether the line limit clipped this result — explains a missing selection. */
  truncated?: boolean
  /** The server-side keyword currently in effect, offered back as "keep keyword". */
  currentMatch?: string
  /**
   * Re-run the page's query with a new window / keyword. Only these two change:
   * component, container and limit stay as the page has them. Omit to hide the
   * re-query form (a page that can't re-query shouldn't offer it).
   */
  onRequery?: (next: LogRequery) => void
  /**
   * Called when a line is selected, so the page can turn a RELATIVE window (a
   * preset like "last 5m") into the absolute bounds it actually queried. Without
   * it, a link carrying `sel` would resolve against a different window for the next
   * person to open it — and the line would legitimately not be there.
   */
  onPinTimeRange?: () => void
}) {
  const { t } = useTranslation()
  const { handleCopyWithText } = useCopyToClipboardWithText()

  // ── selected line ──
  // Two sources, and the distinction matters. The INDEX is authoritative for a
  // selection made here (a click or an arrow key already knows which row it hit —
  // re-deriving that through the key would be a round-trip that can only lose).
  // The `sel` KEY is what persists it into the URL for a reload / shared link, and
  // is resolved back to an index only when it did NOT come from this session.
  const [sel, setSel] = useQueryState("sel", parseAsString)
  const [selIndex, setSelIndex] = useState(-1)
  const ownKey = useRef<string | null>(null)

  const lineIndex = useMemo(() => buildLineIndex(entries), [entries])

  // A new result set invalidates "this selection is mine": the same key now has to
  // prove it still resolves (a re-query may have dropped the line).
  //
  // APPENDING is not a new result set. A streaming source hands over a longer
  // array every batch, and treating each one as a replacement re-resolves the
  // selection mid-stream — against an index that is still being rebuilt, which
  // reports the line as missing while it is plainly on screen. Same first
  // element and no rows lost means the earlier rows, and the selection into
  // them, are untouched.
  const prevEntries = useRef<LogEntry[]>(entries)
  useEffect(() => {
    const prev = prevEntries.current
    prevEntries.current = entries
    const appended = entries.length >= prev.length && (prev.length === 0 || entries[0] === prev[0])
    if (!appended) ownKey.current = null
  }, [entries])

  useEffect(() => {
    if (sel && sel === ownKey.current) return
    setSelIndex(sel ? findLineByKey(lineIndex, sel) : -1)
  }, [lineIndex, sel])

  const selected = selIndex >= 0 ? entries[selIndex] : undefined
  // Shown only for a key that came from the URL and no longer resolves — a
  // re-query whose window or line limit dropped the line. A selection made in this
  // session can never land here, which is why the click path sets the index
  // directly.
  const selMissing = !!sel && selIndex < 0 && !isLoading && entries.length > 0

  // Wrapping is refused past the cap: the setting stays in the URL (so it applies
  // again once the result is small enough) but this view renders unwrapped.
  const wrapping = wrap && entries.length <= WRAP_MAX_LINES

  // ── viewport ──
  const scrollRef = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [viewportHeight, setViewportHeight] = useState(0)
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setViewportHeight(el.clientHeight))
    ro.observe(el)
    setViewportHeight(el.clientHeight)
    return () => ro.disconnect()
  }, [])

  const totalHeight = entries.length * ROW_HEIGHT

  /** The row element for an index — wrapped rows are all mounted, so this hits. */
  const rowElement = useCallback(
    (index: number) =>
      scrollRef.current?.querySelector<HTMLElement>(`[data-row="${index}"]`) ?? null,
    [],
  )

  /**
   * Index of the row occupying `top` pixels. Arithmetic when unwrapped; when
   * wrapped, a binary search over the mounted rows' real offsets — measured, never
   * estimated, which is the whole reason wrapped mode does not window.
   */
  const rowAt = useCallback(
    (top: number) => {
      if (!wrapping) return Math.floor(top / ROW_HEIGHT)
      const el = scrollRef.current
      if (!el) return 0
      const rows = el.querySelectorAll<HTMLElement>("[data-row]")
      let lo = 0
      let hi = rows.length - 1
      let found = 0
      while (lo <= hi) {
        const mid = (lo + hi) >> 1
        const row = rows[mid]
        if (row.offsetTop + row.offsetHeight <= top) lo = mid + 1
        else {
          found = mid
          hi = mid - 1
        }
      }
      return found
    },
    [wrapping],
  )

  const scrollToRow = useCallback(
    (index: number, mode: "center" | "nearest" = "center") => {
      const el = scrollRef.current
      if (!el || index < 0) return
      let top = index * ROW_HEIGHT
      let height = ROW_HEIGHT
      if (wrapping) {
        const row = rowElement(index)
        if (!row) return
        top = row.offsetTop
        height = row.offsetHeight
      }
      if (mode === "nearest") {
        if (top < el.scrollTop) el.scrollTop = top
        else if (top + height > el.scrollTop + el.clientHeight) {
          el.scrollTop = top + height - el.clientHeight
        }
        return
      }
      el.scrollTop = Math.max(0, top - Math.floor(el.clientHeight / 3))
    },
    [wrapping, rowElement],
  )

  // ── client-side highlight search over ALL loaded lines ──
  const [query, setQuery] = useState("")
  const [debounced, setDebounced] = useState("")
  const [activeMatch, setActiveMatch] = useState(0)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(query.length >= 2 ? query : ""), 250)
    return () => clearTimeout(id)
  }, [query])

  const search = useMemo(() => {
    const byRow = new Map<number, number[]>()
    const flat: { row: number; start: number }[] = []
    if (!debounced) return { byRow, flat, capped: false }
    const needle = debounced.toLowerCase()
    let capped = false
    for (let i = 0; i < entries.length && !capped; i += 1) {
      const hay = entries[i].log.toLowerCase()
      let from = 0
      for (;;) {
        const at = hay.indexOf(needle, from)
        if (at === -1) break
        const starts = byRow.get(i)
        if (starts) starts.push(at)
        else byRow.set(i, [at])
        flat.push({ row: i, start: at })
        from = at + needle.length
        if (flat.length >= MAX_MATCHES) {
          capped = true
          break
        }
      }
    }
    return { byRow, flat, capped }
  }, [entries, debounced])

  // Reset the cursor when the match set changes. This is synchronising one
  // piece of state with another, not an escape hatch: keeping index 5 after a
  // new query has three matches would scroll to a row that no longer exists.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setActiveMatch(0), [debounced, entries])
  useEffect(() => {
    const hit = search.flat[activeMatch]
    if (hit) scrollToRow(hit.row)
  }, [activeMatch, search, scrollToRow])

  const step = useCallback(
    (direction: 1 | -1) => {
      const total = search.flat.length
      if (total === 0) return
      setActiveMatch((i) => (i + direction + total) % total)
    },
    [search.flat.length],
  )

  // ── position on new results: the selected line if we can still see it, else the
  // newest line (the server returns oldest→newest) ──
  useEffect(() => {
    if (entries.length === 0) return
    if (selIndex >= 0) scrollToRow(selIndex)
    else if (!sel) scrollToRow(entries.length - 1)
    // Intentionally keyed on the result set only: re-scrolling on every selection
    // change would fight the user's own scrolling.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entries])

  // Opening the detail panel shrinks this viewport, which can push the row the
  // user just clicked out of sight. Pull it back once the new height is known.
  useEffect(() => {
    if (selIndex >= 0) scrollToRow(selIndex, "nearest")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewportHeight])

  // ── row content ──
  // Colored segments per line, cached: a scroll re-renders the same rows many
  // times over, and re-tokenizing a 2KB line on every frame is pure waste. Keyed
  // on the line text and dropped whenever the result set changes.
  const styleCache = useRef(new Map<string, StyledLine>())
  useEffect(() => {
    styleCache.current.clear()
  }, [entries])
  const styleOf = useCallback((log: string): StyledLine => {
    const hit = styleCache.current.get(log)
    if (hit) return hit
    const built = styleLine(log)
    styleCache.current.set(log, built)
    return built
  }, [])

  const maxChars = useMemo(() => {
    let max = 0
    for (const e of entries) max = Math.max(max, displayWidth(e.log))
    return max + (showTimestamp ? TS_PREFIX_CHARS : 0)
  }, [entries, showTimestamp])

  // The window of rendered rows — unwrapped mode only; wrapped mode renders all of
  // them. Bounds are in PIXELS so the runway is the same whatever the row height.
  const first = wrapping ? 0 : Math.max(0, Math.floor((scrollTop - OVERSCAN_PX) / ROW_HEIGHT))
  const last = wrapping
    ? entries.length
    : Math.min(entries.length, Math.ceil((scrollTop + viewportHeight + OVERSCAN_PX) / ROW_HEIGHT))
  const activeHit = search.flat[activeMatch]

  // Held in a ref so `selectRow` — and therefore every memoized row's onSelect —
  // keeps a stable identity across the page's re-renders.
  const pinTimeRange = useRef(onPinTimeRange)
  pinTimeRange.current = onPinTimeRange

  const selectRow = useCallback(
    (index: number) => {
      const entry = entries[index]
      if (!entry) return
      const key = logLineKey(entry)
      // The index IS the selection; the key only records it in the URL. Marking the
      // key as ours keeps the resolver from second-guessing a row we just hit.
      ownKey.current = key
      setSelIndex(index)
      void setSel(key)
      // Freeze a relative window into absolute bounds, so the link now in the
      // address bar resolves to this same result for whoever opens it.
      pinTimeRange.current?.()
      scrollToRow(index, "nearest")
    },
    [entries, setSel, scrollToRow],
  )

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      void setSel(null)
      return
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return
    // Own the arrow keys: the default action scrolls the container, which would
    // fight the selection moving.
    e.preventDefault()
    const delta = e.key === "ArrowDown" ? 1 : -1
    const from = selIndex >= 0 ? selIndex : rowAt(scrollTop)
    const next = Math.min(entries.length - 1, Math.max(0, from + delta))
    selectRow(next)
  }

  const handleCopy = () => {
    // A manual selection wins — the user marked exactly what they wanted.
    const picked = window.getSelection()?.toString()
    if (picked && picked.trim()) {
      handleCopyWithText(picked)
      return
    }
    handleCopyWithText(rawText(entries))
  }

  const handleDownload = () => {
    if (entries.length === 0) return
    const blob = new Blob([rawText(entries)], {
      type: "text/plain;charset=utf-8",
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = "component-logs.txt"
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }

  const renderRow = (index: number) => {
    const entry = entries[index]
    return (
      <LogRow
        key={index}
        index={index}
        entry={entry}
        styled={styleOf(entry.log)}
        matchStarts={search.byRow.get(index)}
        matchLength={debounced.length}
        activeStart={activeHit?.row === index ? activeHit.start : undefined}
        selected={index === selIndex}
        wrapped={wrapping}
        showTimestamp={showTimestamp}
        onSelect={selectRow}
      />
    )
  }

  const visible: number[] = []
  for (let i = first; i < last; i += 1) visible.push(i)

  const rows = (
    <div
      ref={scrollRef}
      tabIndex={0}
      onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
      onKeyDown={onKeyDown}
      className={cn(
        // `relative` so a wrapped row's offsetTop is measured against this
        // scroller; `leading-[18px]` keeps one display line equal to ROW_HEIGHT so
        // an unwrapped row's fixed height fits its text exactly.
        "relative h-full w-full px-2 py-1 font-mono text-xs leading-[18px] outline-none",
        // Wrapped rows never need the horizontal axis; unwrapped ones live on it.
        wrapping ? "overflow-x-hidden overflow-y-auto" : "overflow-auto",
      )}
    >
      {wrapping ? (
        // Wrapped: every row in normal flow, heights straight from the browser.
        // No windowing, hence WRAP_MAX_LINES.
        <div className="w-full">{visible.map(renderRow)}</div>
      ) : (
        // Unwrapped: every row is exactly ROW_HEIGHT, so absolute positioning is
        // exact and the container can be as wide as the longest line.
        <div
          className="relative"
          style={{
            height: totalHeight,
            width: `${maxChars}ch`,
            minWidth: "100%",
          }}
        >
          {visible.map(renderRow)}
        </div>
      )}
    </div>
  )

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-border flex h-9 shrink-0 items-center gap-1 border-b px-2">
        {leading}
        <SearchIcon className="text-muted-foreground/60 size-3.5 shrink-0" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") step(e.shiftKey ? -1 : 1)
            if (e.key === "Escape") {
              setQuery("")
              setDebounced("")
            }
          }}
          placeholder={t("componentLogs.highlightPlaceholder")}
          className="text-foreground placeholder:text-muted-foreground/40 min-w-0 flex-1 bg-transparent font-mono text-xs outline-none"
        />
        {debounced && search.flat.length > 0 && (
          <span className="text-muted-foreground/60 shrink-0 font-mono text-xs tabular-nums">
            {activeMatch + 1} / {search.flat.length}
            {search.capped ? "+" : ""}
          </span>
        )}
        {debounced && search.flat.length === 0 && (
          <span className="shrink-0 font-mono text-xs text-red-400/70">
            {t("componentLogs.noMatch")}
          </span>
        )}
        {query && (
          <>
            <button
              onClick={() => step(-1)}
              className="text-muted-foreground hover:text-foreground shrink-0"
              title={t("componentLogs.prevMatch")}
            >
              <ChevronUp className="size-3.5" />
            </button>
            <button
              onClick={() => step(1)}
              className="text-muted-foreground hover:text-foreground shrink-0"
              title={t("componentLogs.nextMatch")}
            >
              <ChevronDown className="size-3.5" />
            </button>
            <button
              onClick={() => {
                setQuery("")
                setDebounced("")
              }}
              className="text-muted-foreground hover:text-foreground shrink-0"
              title={t("componentLogs.clearSearch")}
            >
              <X className="size-3.5" />
            </button>
          </>
        )}
        <div className="bg-border mx-1 h-4 w-px shrink-0" />
        {onWrapChange && (
          <Button
            variant="ghost"
            size="icon"
            className={cn("size-7 shrink-0", wrapping && "text-foreground bg-muted")}
            onClick={() => onWrapChange(!wrap)}
            // Past WRAP_MAX_LINES this view renders unwrapped no matter what the
            // setting says, so offering the toggle there would be a control that
            // does nothing.
            disabled={entries.length > WRAP_MAX_LINES}
            title={
              entries.length > WRAP_MAX_LINES
                ? t("componentLogs.wrapUnavailable")
                : t("componentLogs.wrap")
            }
            aria-pressed={wrapping}
          >
            <WrapTextIcon className="size-3.5" />
          </Button>
        )}
        <span className="text-muted-foreground/60 shrink-0 font-mono text-xs">
          {entries.length} {t("componentLogs.lines")}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={handleCopy}
          disabled={entries.length === 0}
          title={t("componentLogs.copy")}
        >
          <CopyIcon className="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={handleDownload}
          disabled={entries.length === 0}
          title={t("componentLogs.download")}
        >
          <DownloadIcon className="size-3.5" />
        </Button>
      </div>

      {selMissing && (
        <div className="border-border text-muted-foreground flex shrink-0 items-center gap-2 border-b px-2 py-1 text-xs">
          <span>
            {truncated
              ? t("componentLogs.detail.missingTruncated")
              : t("componentLogs.detail.missing")}
          </span>
          <button onClick={() => void setSel(null)} className="hover:text-foreground underline">
            {t("componentLogs.detail.dismiss")}
          </button>
        </div>
      )}

      <div className="relative min-h-0 flex-1">
        {/* Conditional panels are safe here because both carry a stable id: the
            rows panel stays mounted (and keeps its scroll position) when the
            detail panel appears. */}
        <ResizablePanelGroup orientation="vertical" className="h-full">
          <ResizablePanel id="log-rows" defaultSize="40%" minSize="15%">
            {rows}
          </ResizablePanel>
          {selected && (
            <>
              <ResizableHandle withHandle />
              {/* The detail is the bigger half once opened: a structured line's
                  fields plus an unfolded JSON payload need the room, and the
                  list is one drag away. */}
              <ResizablePanel id="log-detail" defaultSize="60%" minSize="20%">
                <LogDetailPanel
                  entry={selected}
                  currentMatch={currentMatch}
                  onClose={() => void setSel(null)}
                  onRequery={
                    onRequery &&
                    ((next, keepHighlight) => {
                      if (!keepHighlight) void setSel(null)
                      onRequery(next)
                    })
                  }
                />
              </ResizablePanel>
            </>
          )}
        </ResizablePanelGroup>
        {isLoading && (
          <div className="bg-background/60 absolute inset-0 flex items-center justify-center text-sm">
            {t("componentLogs.loading")}
          </div>
        )}
        {!isLoading && entries.length === 0 && (
          <div className="text-muted-foreground pointer-events-none absolute inset-0 flex items-center justify-center text-sm">
            {t("componentLogs.empty")}
          </div>
        )}
      </div>
    </div>
  )
}

// Fullwidth / CJK ranges: these code points occupy TWO character cells in a
// monospace font. The row container is sized in `ch`, so measuring UTF-16 length
// alone would under-size a line containing them and leave its tail unreachable by
// horizontal scrolling.
const WIDE_CHARS =
  /[\u1100-\u115f\u2e80-\ua4cf\uac00-\ud7a3\uf900-\ufaff\ufe30-\ufe6f\uff00-\uff60\uffe0-\uffe6]/g

/** Character cells a line occupies. ASCII (the common case) skips the scan. */
function displayWidth(line: string): number {
  if (!/[^\u0000-\u007f]/.test(line)) return line.length
  return line.length + (line.match(WIDE_CHARS)?.length ?? 0)
}

/** The clipboard/download body: the raw lines, in the order shown. */
function rawText(entries: LogEntry[]): string {
  if (entries.length === 0) return ""
  return entries.map((e) => e.log.replace(/\r?\n$/, "")).join("\n") + "\n"
}

/**
 * One log line. Memoized, which matters most in wrapped mode: every row is
 * mounted there, so without this a selection change or a keystroke in the search
 * box would re-render (and re-lay-out) the whole list. Every prop is either a
 * primitive or a value cached upstream (the styled line, the per-row match array),
 * so the comparison actually skips work.
 */
const LogRow = memo(function LogRow({
  index,
  entry,
  styled,
  matchStarts,
  matchLength,
  activeStart,
  selected,
  wrapped,
  showTimestamp,
  onSelect,
}: {
  index: number
  entry: LogEntry
  styled: StyledLine
  matchStarts: number[] | undefined
  matchLength: number
  activeStart: number | undefined
  selected: boolean
  wrapped: boolean
  showTimestamp: boolean
  onSelect: (index: number) => void
}) {
  return (
    <div
      data-row={index}
      onClick={() => onSelect(index)}
      className={cn(
        "cursor-pointer",
        wrapped
          ? // Character-level breaking: a log line's long tokens (ids, URLs,
            // serialized JSON) have no useful word boundaries to break at.
            "break-all whitespace-pre-wrap"
          : "absolute left-0 flex w-full items-center whitespace-pre",
        "hover:bg-muted/60",
        selected && "bg-primary/15 hover:bg-primary/20",
      )}
      style={wrapped ? { minHeight: ROW_HEIGHT } : { top: index * ROW_HEIGHT, height: ROW_HEIGHT }}
    >
      {showTimestamp && entry.timestamp && (
        <span className="text-muted-foreground/60">
          {formatLocalTimestamp(entry.timestamp, { withMillis: true })}{" "}
        </span>
      )}
      <span>{renderLine(styled, matchStarts, matchLength, activeStart)}</span>
    </div>
  )
})

// ── line styling ────────────────────────────────────────────────────────────

// Syntax colors for a klog line: foreground / green / gray, and nothing else.
// The message — the sentence a human reads — is plain foreground; field KEYS are
// the one accent, because they are what you scan a long kv tail for; everything
// that is metadata rather than content (clock, thread, source, and the field
// values) shares one gray. Severity keeps its own color, and a failure value
// reuses that same red: those two are signals, not decoration. One font weight
// throughout. Light shades are darker than the dark-mode ones on purpose — a
// single mid-tone shared by both themes is unreadable on white.
const LEVEL_COLOR: Record<LogLevel, string> = {
  info: "text-sky-700 dark:text-sky-400",
  warning: "text-amber-700 dark:text-amber-400",
  error: "text-red-700 dark:text-red-400",
  fatal: "text-red-800 dark:text-red-300",
}
// One gray for everything that is context rather than the sentence: the clock,
// the thread id, the `file.go:NN]` location, and the field values.
const META_GRAY = "text-muted-foreground/70"
const TOKEN_COLOR: Record<TokenKind, string> = {
  level: "",
  time: META_GRAY,
  thread: META_GRAY,
  source: META_GRAY,
  message: "text-foreground",
  key: "text-teal-600 dark:text-teal-300",
  value: META_GRAY,
  errorValue: "text-red-700 dark:text-red-400",
}

// ANSI's 8 colors, mapped to shades that stay readable on both themes.
const ANSI_COLOR: Record<string, string> = {
  black: "text-zinc-500",
  red: "text-red-500",
  green: "text-emerald-500",
  yellow: "text-amber-500",
  blue: "text-sky-500",
  magenta: "text-fuchsia-500",
  cyan: "text-cyan-500",
  white: "text-zinc-600 dark:text-zinc-300",
}

/** A line ready to render: escape-free text plus the class of each styled span. */
interface StyledLine {
  text: string
  spans: { start: number; end: number; className: string }[]
}

/**
 * Style one raw line. A line that carries its own ANSI colors keeps them — the
 * producer chose those deliberately, so they win over our syntax highlighting;
 * everything else is highlighted as klog.
 */
function styleLine(raw: string): StyledLine {
  const { text, spans } = ansiSpans(raw)
  if (spans.length > 0) {
    return {
      text,
      spans: spans.map((s) => ({
        start: s.start,
        end: s.end,
        className: cn(
          s.color && ANSI_COLOR[s.color],
          s.bright && "brightness-125",
          s.bold && "font-semibold",
          s.dim && "opacity-60",
          s.italic && "italic",
          s.underline && "underline",
        ),
      })),
    }
  }
  return {
    text,
    spans: tokenizeLogLine(text).map((tk) => ({
      start: tk.start,
      end: tk.end,
      className:
        tk.kind === "level" ? (tk.level && LEVEL_COLOR[tk.level]) || "" : TOKEN_COLOR[tk.kind],
    })),
  }
}

/**
 * Render a line as spans: the styling above, with the search matches marked on
 * top. Both are ranges over the same string, so the two are merged at their
 * boundaries rather than one overwriting the other — a match inside a value stays
 * the value's color AND gets the highlight.
 */
function renderLine(
  line: StyledLine,
  matchStarts: number[] | undefined,
  matchLength: number,
  activeStart: number | undefined,
): ReactNode {
  const { text, spans } = line
  const matches =
    matchStarts && matchLength > 0
      ? matchStarts.map((start) => ({ start, end: start + matchLength }))
      : []
  if (spans.length === 0 && matches.length === 0) return text

  // Every range boundary becomes a cut point; each resulting segment then has one
  // style and one match state.
  const cuts = new Set<number>([0, text.length])
  for (const r of [...spans, ...matches]) {
    if (r.start > 0 && r.start < text.length) cuts.add(r.start)
    if (r.end > 0 && r.end < text.length) cuts.add(r.end)
  }
  const points = [...cuts].sort((a, b) => a - b)

  const out: ReactNode[] = []
  for (let i = 0; i < points.length - 1; i += 1) {
    const from = points[i]
    const to = points[i + 1]
    if (to <= from) continue
    const chunk = text.slice(from, to)
    const style = spans.find((s) => s.start <= from && s.end >= to)
    const match = matches.find((m) => m.start <= from && m.end >= to)
    if (!style && !match) {
      out.push(chunk)
      continue
    }
    if (match) {
      out.push(
        <mark
          key={from}
          className={cn(
            "rounded-sm bg-amber-500/30 text-inherit",
            match.start === activeStart && "bg-amber-400 text-black",
            style?.className,
          )}
        >
          {chunk}
        </mark>,
      )
      continue
    }
    out.push(
      <span key={from} className={style?.className}>
        {chunk}
      </span>,
    )
  }
  return out
}
