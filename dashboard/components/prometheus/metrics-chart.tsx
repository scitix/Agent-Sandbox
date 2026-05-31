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
 * MetricsChart — Reusable Prometheus time-series line chart component.
 *
 * Extracted from admin/page.tsx PrometheusChart, extended with configurable
 * height and empty message. Used by the admin page and SandboxMetricsSheet.
 *
 * Supports Grafana-style drag-to-zoom: when `onTimeRangeSelect` is provided,
 * the user can click-and-drag horizontally inside the chart to select a time
 * range. On mouse-up the callback is fired with an absolute TimeRangeValue.
 */

import { useState, useCallback, useMemo, useEffect, useRef } from "react"
import { useTranslation } from "@/lib/i18n"
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceArea,
} from "recharts"
type TooltipEntry = { name?: string | number; value?: unknown; color?: string }
import { cn } from "@/lib/utils"
import { mergeChartSeries, type RechartsDataPoint } from "@/lib/prometheus/transform"
import type { ChartSeries, TimeRangeValue } from "@/lib/types/prometheus"
import { Card } from "../ui/card"
import { Loader2, Maximize2, CheckCheck, Copy } from "lucide-react"
import {
  Tooltip as UITooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@/components/ui/tooltip"
import { Button } from "@/components/ui/button"
import { LargeDialog } from "@/components/large-dialog"
import { useAtomValue } from "jotai"
import { isAdminAtom } from "@/lib/atoms"

// ─── Series config ────────────────────────────────────────────────────────

export interface SeriesConfig {
  name: string
  color?: string
}

const PALETTE = [
  "#6366f1", // indigo
  "#22c55e", // green
  "#ef4444", // red
  "#f59e0b", // amber
  "#3b82f6", // blue
  "#a855f7", // purple
  "#14b8a6", // teal
  "#f97316", // orange
  "#ec4899", // pink
  "#84cc16", // lime
  "#eab308", // yellow
  "#06b6d4", // cyan
  "#8b5cf6", // violet
  "#10b981", // emerald
]

function resolveColor(cfg: SeriesConfig, index: number): string {
  return cfg.color ?? PALETTE[index % PALETTE.length]
}

function normalizeSeries(raw: (string | SeriesConfig)[]): SeriesConfig[] {
  return raw.map((s) => (typeof s === "string" ? { name: s } : s))
}

// ─── Custom tooltip ───────────────────────────────────────────────────────

function TimeSeriesTooltip({
  active,
  payload,
  label,
  valueFormatter,
}: {
  active?: boolean
  payload?: readonly TooltipEntry[]
  label?: number | string
  valueFormatter?: (v: number) => string
}) {
  if (!active || !payload?.length) return null
  const ts = typeof label === "number" ? new Date(label) : null
  return (
    <div className="bg-popover border-border rounded border px-3 py-2 shadow-md">
      {ts && (
        <p className="text-muted-foreground mb-1.5 font-mono text-xs">
          {ts.toLocaleDateString([], { month: "short", day: "numeric" })}{" "}
          {ts.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}
        </p>
      )}
      {payload.map((entry) => (
        <div
          key={entry.name}
          className="flex flex-row items-center justify-between font-mono text-xs"
          style={{ color: entry.color }}
        >
          <span>{entry.name}:</span>
          <span className="text-right">
            {valueFormatter
              ? valueFormatter(entry.value as number)
              : (entry.value as number).toFixed(3)}
          </span>
        </div>
      ))}
    </div>
  )
}

// ─── Series stats computation ─────────────────────────────────────────────

interface SeriesStats {
  mean: number
  max: number
  min: number
}

function computeSeriesStats(
  data: RechartsDataPoint[],
  seriesNames: SeriesConfig[],
): Record<string, SeriesStats> {
  const result: Record<string, SeriesStats> = {}
  for (const { name } of seriesNames) {
    const values = data
      .map((d) => d[name])
      .filter((v): v is number => typeof v === "number" && !isNaN(v))
    if (values.length === 0) {
      result[name] = { mean: 0, max: 0, min: 0 }
      continue
    }
    const sum = values.reduce((acc, v) => acc + v, 0)
    result[name] = {
      mean: sum / values.length,
      max: Math.max(...values),
      min: Math.min(...values),
    }
  }
  return result
}

// ─── Custom Legend ────────────────────────────────────────────────────────

interface LegendPayloadItem {
  value: string
  color: string
  dataKey?: string
}

interface CustomLegendProps {
  payload?: LegendPayloadItem[]
  disabledSeries: Set<string>
  onLegendClick: (name: string, shiftKey: boolean) => void
}

function CustomLegend({ payload, disabledSeries, onLegendClick }: CustomLegendProps) {
  if (!payload?.length) return null
  return (
    <div className="mt-2 flex flex-wrap justify-center gap-x-4 gap-y-1">
      {payload.map((entry) => {
        const name = entry.value
        const isDisabled = disabledSeries.has(name)
        return (
          <button
            key={name}
            type="button"
            className="flex cursor-pointer items-center gap-1.5 select-none"
            style={{ opacity: isDisabled ? 0.35 : 1 }}
            onClick={(e) => onLegendClick(name, e.shiftKey)}
          >
            <svg width="16" height="8" className="shrink-0">
              <line
                x1="0"
                y1="4"
                x2="16"
                y2="4"
                stroke={isDisabled ? "#9ca3af" : entry.color}
                strokeWidth="1.5"
              />
              <circle
                cx="8"
                cy="4"
                r="2.5"
                fill="var(--card)"
                stroke={isDisabled ? "#9ca3af" : entry.color}
                strokeWidth="1.5"
              />
            </svg>
            <span
              className="font-mono text-[11px]"
              style={{ color: isDisabled ? "#9ca3af" : "inherit" }}
            >
              {name}
            </span>
          </button>
        )
      })}
    </div>
  )
}

// ─── Legend Table (expanded dialog) ──────────────────────────────────────

interface LegendTableProps {
  payload?: LegendPayloadItem[]
  disabledSeries: Set<string>
  onLegendClick: (name: string, shiftKey: boolean) => void
  stats: Record<string, SeriesStats>
  valueFormatter?: (v: number) => string
  labelName: string
  labelMean: string
  labelMax: string
  labelMin: string
  /** Per-row PromQL — indexed to match `payload`. Row i shows a copy button for promql[i]. */
  promql?: string[]
  copiedIndex?: number | null
  onCopyPromql?: (query: string, index: number) => void
  copyLabel?: string
  copiedLabel?: string
}

function LegendTable({
  payload,
  disabledSeries,
  onLegendClick,
  stats,
  valueFormatter,
  labelName,
  labelMean,
  labelMax,
  labelMin,
  promql,
  copiedIndex,
  onCopyPromql,
  copyLabel,
  copiedLabel,
}: LegendTableProps) {
  if (!payload?.length) return null
  const fmt = (v: number) => (valueFormatter ? valueFormatter(v) : v.toFixed(3))
  const showPromqlCol = !!promql && promql.length > 0 && !!onCopyPromql
  return (
    <div className="mt-3 w-full">
      <table className="w-full border-collapse font-mono text-xs">
        <thead>
          <tr className="text-muted-foreground border-border border-b">
            <th className="pb-1 text-left font-normal">{labelName}</th>
            <th className="pr-3 pb-1 text-right font-normal">{labelMean}</th>
            <th className="pr-3 pb-1 text-right font-normal">{labelMax}</th>
            <th className="pb-1 text-right font-normal">{labelMin}</th>
            {showPromqlCol && <th className="w-8 pb-1" />}
          </tr>
        </thead>
        <tbody>
          {payload.map((entry, i) => {
            const name = entry.value
            const isDisabled = disabledSeries.has(name)
            const s = stats[name] ?? { mean: 0, max: 0, min: 0 }
            const rowQuery = showPromqlCol ? promql![i] : undefined
            const isCopied = copiedIndex === i
            return (
              <tr
                key={name}
                className="hover:bg-muted/40 cursor-pointer transition-colors"
                style={{ opacity: isDisabled ? 0.35 : 1 }}
                onClick={(e) => onLegendClick(name, e.shiftKey)}
              >
                <td className="py-0.5 pr-4">
                  <div className="flex items-center gap-2">
                    <svg width="16" height="8" className="shrink-0">
                      <line
                        x1="0"
                        y1="4"
                        x2="16"
                        y2="4"
                        stroke={isDisabled ? "#9ca3af" : entry.color}
                        strokeWidth="1.5"
                      />
                      <circle
                        cx="8"
                        cy="4"
                        r="2.5"
                        fill="var(--background)"
                        stroke={isDisabled ? "#9ca3af" : entry.color}
                        strokeWidth="1.5"
                      />
                    </svg>
                    <span style={{ color: isDisabled ? "#9ca3af" : entry.color }}>{name}</span>
                  </div>
                </td>
                <td className="py-0.5 pr-3 text-right tabular-nums">{fmt(s.mean)}</td>
                <td className="py-0.5 pr-3 text-right tabular-nums">{fmt(s.max)}</td>
                <td className="py-0.5 text-right tabular-nums">{fmt(s.min)}</td>
                {showPromqlCol && (
                  <td className="py-0.5 pl-2 text-right" onClick={(e) => e.stopPropagation()}>
                    {rowQuery ? (
                      <TooltipProvider delay={400}>
                        <UITooltip>
                          <TooltipTrigger
                            render={
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                className="text-muted-foreground hover:text-foreground"
                                onClick={() => onCopyPromql!(rowQuery, i)}
                              />
                            }
                          >
                            {isCopied ? (
                              <CheckCheck className="size-3.5 text-green-500" />
                            ) : (
                              <Copy className="size-3.5" />
                            )}
                            <span className="sr-only">{isCopied ? copiedLabel : copyLabel}</span>
                          </TooltipTrigger>
                          <TooltipContent side="left" className="max-w-md">
                            <p className="font-mono break-all">{rowQuery}</p>
                          </TooltipContent>
                        </UITooltip>
                      </TooltipProvider>
                    ) : null}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

// ─── ChartBody ─────────────────────────────────────────────────────────────
// Internal component that renders the Recharts LineChart.
// Used by both the card view and the fullscreen expand dialog.

interface ChartBodyProps {
  data: RechartsDataPoint[]
  series: SeriesConfig[]
  /** Fixed pixel height. Ignored when `fillHeight` is true. */
  height: number
  /**
   * When true, the chart fills the parent's remaining flex height instead of using `height`.
   * The parent must be a flex column with a bounded height; the chart area uses `flex-1 min-h-0`.
   * Used by the fullscreen expand dialog so chart + legend table fit without scrollbars.
   */
  fillHeight?: boolean
  valueFormatter?: (v: number) => string
  yAxisLabel?: string
  disabledSeries: Set<string>
  onLegendClick: (name: string, shiftKey: boolean) => void
  onTimeRangeSelect?: (range: TimeRangeValue) => void
  /** Optional fixed X-axis domain (unix seconds). When both are set, XAxis stays pinned even with sparse data. */
  xStart?: number
  xEnd?: number
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  handleMouseDown: (nextState: any) => void
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  handleMouseMove: (nextState: any) => void
  handleMouseUp: () => void
  isDragging: boolean
  dragStart: number | null
  dragEnd: number | null
  /** When true, renders the Grafana-style stats table legend below the chart */
  legendTable?: {
    stats: Record<string, SeriesStats>
    labelName: string
    labelMean: string
    labelMax: string
    labelMin: string
    promql?: string[]
    copiedIndex?: number | null
    onCopyPromql?: (query: string, index: number) => void
    copyLabel?: string
    copiedLabel?: string
  }
}

function ChartBody({
  data,
  series,
  height,
  fillHeight,
  valueFormatter,
  yAxisLabel,
  disabledSeries,
  onLegendClick,
  onTimeRangeSelect,
  xStart,
  xEnd,
  handleMouseDown,
  handleMouseMove,
  handleMouseUp,
  isDragging,
  dragStart,
  dragEnd,
  legendTable,
}: ChartBodyProps) {
  // Snap fixed X-axis bounds outward to the nearest whole minute so ticks
  // land on :00 marks. The tick formatter only shows HH:MM, so a 23 s offset
  // in the raw preset (e.g. end = now) would produce labels like 14:07, 14:22,
  // 14:37; snapping gives the expected 14:00, 14:15, 14:30 cadence.
  const snappedStart = xStart != null ? Math.floor(xStart / 60) * 60 : undefined
  const snappedEnd = xEnd != null ? Math.ceil(xEnd / 60) * 60 : undefined
  const xDomain: [number | string, number | string] =
    snappedStart != null && snappedEnd != null
      ? [snappedStart * 1000, snappedEnd * 1000]
      : ["dataMin", "dataMax"]
  const hasFixedDomain = snappedStart != null && snappedEnd != null

  // When fillHeight is true, measure the chart slot height ourselves and pass
  // a concrete pixel value to ResponsiveContainer. Recharts's "100%" height
  // fails inside a flex-1 parent when the flex layout hasn't settled yet on
  // the first paint, which leaves the chart at 0px until a resize event.
  const chartSlotRef = useRef<HTMLDivElement | null>(null)
  const [slotHeight, setSlotHeight] = useState<number>(0)
  useEffect(() => {
    if (!fillHeight) return
    const el = chartSlotRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      const h = entries[0]?.contentRect.height ?? 0
      setSlotHeight(Math.max(0, Math.floor(h)))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [fillHeight])

  const effectiveHeight = fillHeight ? slotHeight : height
  return (
    <div className={fillHeight ? "flex h-full flex-col" : undefined}>
      <div ref={chartSlotRef} className={fillHeight ? "min-h-0 flex-1" : undefined}>
        {effectiveHeight > 0 && (
          <ResponsiveContainer width="100%" height={effectiveHeight}>
            <LineChart
              data={data}
              margin={{ top: 4, right: 16, bottom: 4, left: 8 }}
              onMouseDown={onTimeRangeSelect ? handleMouseDown : undefined}
              onMouseMove={onTimeRangeSelect ? handleMouseMove : undefined}
              onMouseUp={onTimeRangeSelect ? handleMouseUp : undefined}
              onMouseLeave={onTimeRangeSelect ? handleMouseUp : undefined}
              className={onTimeRangeSelect ? "cursor-crosshair select-none" : undefined}
            >
              <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
              <XAxis
                dataKey="time"
                type="number"
                domain={xDomain}
                allowDataOverflow={hasFixedDomain}
                scale="time"
                tickFormatter={(v: number) =>
                  new Date(v).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                }
                className="font-mono"
                tick={{ fontSize: 10 }}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                className="font-mono"
                tick={{ fontSize: 10 }}
                tickLine={false}
                axisLine={false}
                tickFormatter={valueFormatter}
                label={
                  yAxisLabel
                    ? { value: yAxisLabel, angle: -90, position: "insideLeft", fontSize: 10 }
                    : undefined
                }
              />
              <Tooltip
                content={(props) => (
                  <TimeSeriesTooltip {...props} valueFormatter={valueFormatter} />
                )}
              />
              {legendTable ? null : (
                <Legend
                  content={
                    <CustomLegend disabledSeries={disabledSeries} onLegendClick={onLegendClick} />
                  }
                />
              )}
              {series.map((cfg, i) => (
                <Line
                  key={cfg.name}
                  type="monotone"
                  dataKey={cfg.name}
                  stroke={resolveColor(cfg, i)}
                  dot={false}
                  strokeWidth={1.5}
                  connectNulls
                  hide={disabledSeries.has(cfg.name)}
                />
              ))}
              {/* Drag-to-zoom selection highlight */}
              {isDragging && dragStart !== null && dragEnd !== null && (
                <ReferenceArea
                  x1={Math.min(dragStart, dragEnd)}
                  x2={Math.max(dragStart, dragEnd)}
                  fill="#6366f1"
                  fillOpacity={0.25}
                  stroke="#6366f1"
                  strokeOpacity={0.6}
                />
              )}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
      {legendTable && (
        <LegendTable
          payload={series.map((cfg, i) => ({
            value: cfg.name,
            color: resolveColor(cfg, i),
          }))}
          disabledSeries={disabledSeries}
          onLegendClick={onLegendClick}
          stats={legendTable.stats}
          valueFormatter={valueFormatter}
          labelName={legendTable.labelName}
          labelMean={legendTable.labelMean}
          labelMax={legendTable.labelMax}
          labelMin={legendTable.labelMin}
          promql={legendTable.promql}
          copiedIndex={legendTable.copiedIndex}
          onCopyPromql={legendTable.onCopyPromql}
          copyLabel={legendTable.copyLabel}
          copiedLabel={legendTable.copiedLabel}
        />
      )}
    </div>
  )
}

// ─── MetricsChart ─────────────────────────────────────────────────────────

/**
 * Raw BFF response envelope for a time-series Prometheus endpoint.
 * When passed as `response`, MetricsChart extracts series + PromQL automatically,
 * so callers don't need to pre-merge or wire PromQL through separately.
 */
export interface MetricsChartResponse {
  configured?: boolean
  data?: { series: ChartSeries[] }
  promql?: string[]
}

export interface MetricsChartProps {
  title: string
  /**
   * Optional description.
   * - In the card view: shown as a tooltip on the title (cursor-help)
   * - In the expand dialog: shown as a subtitle below the title
   */
  description?: string
  /** Series to render. Each entry is either a name string (color auto-assigned from palette) or a { name, color } object */
  series: (string | SeriesConfig)[]
  /**
   * Raw BFF response object. When set, the chart runs mergeChartSeries
   * internally on `response.data.series` and wires `response.promql` into
   * the admin Copy PromQL buttons — no prop-drilling needed.
   * Takes precedence over the legacy `data` prop.
   */
  response?: MetricsChartResponse
  /**
   * Legacy: pre-merged Recharts data points (output of mergeChartSeries()).
   * Prefer `response` for new code — this exists only for older callers
   * that pre-merge series for shared state across multiple views.
   */
  data?: RechartsDataPoint[]
  /**
   * True only on the very first load (no cached data yet). Shows a skeleton
   * placeholder and hides the chart completely.
   * Use React Query's `isLoading` (not `isFetching`) — this prevents the chart
   * from flickering when the time range changes and a background refetch starts.
   */
  isLoading: boolean
  /**
   * True whenever a background refetch is in progress (including time-range
   * changes). When true, a small spinner is shown in the card header so the
   * user knows fresh data is coming, but the previous data stays visible.
   * Pass React Query's `isFetching` here.
   */
  isFetching?: boolean
  /** Formatter applied to Y-axis ticks and tooltip values */
  valueFormatter?: (v: number) => string
  yAxisLabel?: string
  /** Message shown when data is empty. Defaults to "No data" */
  emptyMessage?: string
  height?: number
  className?: string
  /**
   * Optional callback for Grafana-style drag-to-zoom.
   * When provided, the user can click-and-drag horizontally inside the chart
   * to select a time range. On mouse-up the callback fires with an absolute
   * TimeRangeValue. Enables select-none + crosshair cursor on the chart.
   */
  onTimeRangeSelect?: (range: TimeRangeValue) => void
  /**
   * Fixed X-axis range in unix seconds. When both are provided the XAxis
   * stays pinned to this window even if the series is sparse — useful when
   * a metric only has data for part of the selected time range.
   */
  xStart?: number
  xEnd?: number
  /**
   * When true (default), a fullscreen expand button appears on card hover,
   * allowing the user to open the chart in a large dialog.
   */
  expandable?: boolean
  /**
   * Legacy: explicit PromQL strings for admin copy buttons. Prefer `response`
   * (where `promql` is inferred from the BFF envelope).
   */
  promqlQueries?: string[]
  /**
   * Series names that should be hidden on initial render.
   * The user can still toggle them via the legend. Defaults to none hidden.
   */
  initialDisabledSeries?: string[]
}

export function MetricsChart({
  title,
  description,
  series: rawSeries,
  response,
  data: dataProp,
  isLoading,
  isFetching = false,
  valueFormatter,
  yAxisLabel,
  emptyMessage,
  height = 200,
  className,
  onTimeRangeSelect,
  xStart,
  xEnd,
  expandable = true,
  promqlQueries,
  initialDisabledSeries,
}: MetricsChartProps) {
  const { t } = useTranslation()
  const isAdmin = useAtomValue(isAdminAtom)
  // Set of series names that are currently hidden/disabled
  const [disabledSeries, setDisabledSeries] = useState<Set<string>>(
    () => new Set(initialDisabledSeries ?? []),
  )
  // Fullscreen dialog open state
  const [isExpanded, setIsExpanded] = useState(false)
  // PromQL copy state: tracks which query index was last copied
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null)

  // Drag-to-zoom state (values are X-axis coords in ms)
  const [dragStart, setDragStart] = useState<number | null>(null)
  const [dragEnd, setDragEnd] = useState<number | null>(null)
  const isDragging = dragStart !== null

  // Normalize to SeriesConfig[] once, stable for the render
  const series = normalizeSeries(rawSeries)

  // If the caller passed a raw BFF response, extract series + promql from it.
  // Otherwise fall back to the legacy `data` / `promqlQueries` props.
  const data: RechartsDataPoint[] = useMemo(
    () => (response ? mergeChartSeries(response.data?.series ?? []) : (dataProp ?? [])),
    [response, dataProp],
  )
  const effectivePromql = response?.promql ?? promqlQueries

  // Pre-computed stats for the legend table in the expanded dialog
  const seriesStats = computeSeriesStats(data, series)

  // ── Drag-to-zoom event handlers ─────────────────────────────────────────
  // Note: Recharts passes activeLabel as a string even for numeric axes.

  const handleMouseDown = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (nextState: any) => {
      if (!onTimeRangeSelect) return
      const x = nextState?.activeLabel != null ? Number(nextState.activeLabel) : null
      if (x === null || isNaN(x)) return
      setDragStart(x)
      setDragEnd(x)
    },
    [onTimeRangeSelect],
  )

  const handleMouseMove = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (nextState: any) => {
      if (!isDragging || !onTimeRangeSelect) return
      const x = nextState?.activeLabel != null ? Number(nextState.activeLabel) : null
      if (x === null || isNaN(x)) return
      setDragEnd(x)
    },
    [isDragging, onTimeRangeSelect],
  )

  const handleMouseUp = useCallback(() => {
    if (!isDragging || dragStart === null || dragEnd === null || !onTimeRangeSelect) {
      setDragStart(null)
      setDragEnd(null)
      return
    }
    const start = Math.min(dragStart, dragEnd)
    const end = Math.max(dragStart, dragEnd)
    // Require at least 5 seconds of selection to avoid accidental zooms
    if (end - start > 5000) {
      onTimeRangeSelect({
        type: "absolute",
        start: Math.round(start / 1000),
        end: Math.round(end / 1000),
      })
    }
    setDragStart(null)
    setDragEnd(null)
  }, [isDragging, dragStart, dragEnd, onTimeRangeSelect])

  const handleCopyPromql = useCallback((query: string, index: number) => {
    void navigator.clipboard.writeText(query)
    setCopiedIndex(index)
    setTimeout(() => setCopiedIndex(null), 2000)
  }, [])

  const handleLegendClick = useCallback(
    (name: string, shiftKey: boolean) => {
      setDisabledSeries((prev) => {
        const next = new Set(prev)

        if (shiftKey) {
          // Shift+Click: toggle only this series' disabled state
          if (next.has(name)) {
            next.delete(name)
          } else {
            next.add(name)
          }
        } else {
          // Plain click
          const names = series.map((s) => s.name)
          const isOnlyActive = !next.has(name) && names.filter((s) => !next.has(s)).length === 1

          if (isOnlyActive) {
            // Clicking the sole active legend → restore all
            return new Set()
          }

          const wasAlreadySoleActive =
            !next.has(name) && names.every((s) => s === name || next.has(s))

          if (wasAlreadySoleActive) {
            // Clicking the active legend that's the only visible one → restore all
            return new Set()
          }

          // Isolate: disable all except this one
          return new Set(names.filter((s) => s !== name))
        }

        return next
      })
    },
    [series],
  )

  return (
    <>
      <Card className={cn("group bg-card border-border border p-4 shadow-none ring-0", className)}>
        <div className="mb-4 flex items-center justify-between">
          {/* Title with description tooltip when available */}
          <div className="flex items-center gap-1.5">
            {description ? (
              <TooltipProvider delay={300}>
                <UITooltip>
                  <TooltipTrigger
                    render={
                      <h3 className="text-muted-foreground cursor-help font-mono text-xs font-bold tracking-[0.15em] uppercase select-none" />
                    }
                  >
                    {title}
                  </TooltipTrigger>
                  <TooltipContent side="top" className="max-w-xs">
                    {description}
                  </TooltipContent>
                </UITooltip>
              </TooltipProvider>
            ) : (
              <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.15em] uppercase">
                {title}
              </h3>
            )}
          </div>

          {/* Right-side controls — spinner and expand share the same slot */}
          <div className="flex h-6 w-6 items-center justify-center">
            {isFetching && !isLoading ? (
              /* Fetching spinner takes priority over expand button */
              <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
            ) : expandable ? (
              /* Expand button: revealed on card hover */
              <TooltipProvider delay={600}>
                <UITooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground hover:text-foreground opacity-0 transition-opacity group-hover:opacity-100"
                        onClick={() => setIsExpanded(true)}
                      />
                    }
                  >
                    <Maximize2 className="size-3.5" />
                    <span className="sr-only">{t("prometheus.expandChart")}</span>
                  </TooltipTrigger>
                  <TooltipContent side="top">{t("prometheus.expandChart")}</TooltipContent>
                </UITooltip>
              </TooltipProvider>
            ) : null}
          </div>
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center" style={{ height }}>
            <div className="bg-muted h-1 w-24 animate-pulse rounded" />
          </div>
        ) : data.length === 0 ? (
          <div
            className="text-muted-foreground flex items-center justify-center font-mono text-xs"
            style={{ height }}
          >
            {emptyMessage ?? t("common.noData")}
          </div>
        ) : (
          <ChartBody
            data={data}
            series={series}
            height={height}
            valueFormatter={valueFormatter}
            yAxisLabel={yAxisLabel}
            disabledSeries={disabledSeries}
            onLegendClick={handleLegendClick}
            onTimeRangeSelect={onTimeRangeSelect}
            xStart={xStart}
            xEnd={xEnd}
            handleMouseDown={handleMouseDown}
            handleMouseMove={handleMouseMove}
            handleMouseUp={handleMouseUp}
            isDragging={isDragging}
            dragStart={dragStart}
            dragEnd={dragEnd}
          />
        )}
      </Card>

      {/* Fullscreen expand dialog */}
      {expandable && (
        <LargeDialog
          open={isExpanded}
          onOpenChange={setIsExpanded}
          title={title}
          description={description}
          contentClassName="overflow-hidden"
        >
          {data.length === 0 ? (
            <div className="text-muted-foreground flex h-[75dvh] items-center justify-center font-mono text-xs">
              {emptyMessage ?? t("common.noData")}
            </div>
          ) : (
            <div className="h-[75dvh] w-full">
              <ChartBody
                data={data}
                series={series}
                height={0}
                fillHeight
                valueFormatter={valueFormatter}
                yAxisLabel={yAxisLabel}
                disabledSeries={disabledSeries}
                onLegendClick={handleLegendClick}
                onTimeRangeSelect={onTimeRangeSelect}
                xStart={xStart}
                xEnd={xEnd}
                handleMouseDown={handleMouseDown}
                handleMouseMove={handleMouseMove}
                handleMouseUp={handleMouseUp}
                isDragging={isDragging}
                dragStart={dragStart}
                dragEnd={dragEnd}
                legendTable={{
                  stats: seriesStats,
                  labelName: t("prometheus.legendName"),
                  labelMean: t("prometheus.legendMean"),
                  labelMax: t("prometheus.legendMax"),
                  labelMin: t("prometheus.legendMin"),
                  promql: isAdmin ? effectivePromql : undefined,
                  copiedIndex,
                  onCopyPromql: handleCopyPromql,
                  copyLabel: t("prometheus.copyPromql"),
                  copiedLabel: t("prometheus.copiedPromql"),
                }}
              />
            </div>
          )}
        </LargeDialog>
      )}
    </>
  )
}
