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
 * GrafanaTimePicker — Grafana-inspired time range picker and refresh control.
 *
 * Layout (left to right):
 *   [🕐 time-range-display ▼]  [⇔ Zoom Out]  [🔄 countdown ▼]
 *
 * - Time range display: Popover with absolute date picker (left) + quick ranges (right)
 * - Zoom Out: expand time range (preset → next larger preset, absolute → ×2)
 * - Refresh section: click main area to refresh immediately, dropdown to set interval
 *
 * Disabled (locked) mode for terminated sandboxes:
 *   All controls are dimmed, time display shows fixed "start to end" label.
 */

import { useState, useCallback } from "react"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/lib/i18n"
import { Clock, ChevronDown, RefreshCw, ZoomOut, Check, Globe } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover"
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu"
import {
  type TimeRangeValue,
  type RefreshInterval,
  type TimeRangePreset,
  PRESET_ORDER,
  REFRESH_INTERVALS,
  REFRESH_INTERVAL_LABELS,
  formatUnixTimestamp,
  zoomOut,
  computeTimeRange,
} from "@/lib/types/prometheus"

// ─── Preset → i18n key mapping ────────────────────────────────────────────────

const PRESET_LABEL_KEYS: Record<TimeRangePreset, TranslationKey> = {
  "5m": "prometheus.preset.5m",
  "15m": "prometheus.preset.15m",
  "30m": "prometheus.preset.30m",
  "1h": "prometheus.preset.1h",
  "3h": "prometheus.preset.3h",
  "6h": "prometheus.preset.6h",
  "12h": "prometheus.preset.12h",
  "1d": "prometheus.preset.1d",
  "2d": "prometheus.preset.2d",
  "7d": "prometheus.preset.7d",
  "30d": "prometheus.preset.30d",
}

// ─── Types ────────────────────────────────────────────────────────────────────

export interface GrafanaTimePickerProps {
  value: TimeRangeValue
  onValueChange: (v: TimeRangeValue) => void
  refreshInterval: RefreshInterval
  onRefreshIntervalChange: (interval: RefreshInterval) => void
  /** Called when user clicks the refresh button — triggers immediate refetch */
  onRefresh: () => void
  /** Countdown in seconds until next auto-refresh. null = no countdown (Off) */
  countdown?: number | null
  /** When true, the refresh icon spins to indicate an in-flight request */
  isFetching?: boolean
  /** When true, all controls are disabled (e.g. terminated sandbox with fixed time range) */
  disabled?: boolean
  /** When disabled=true, this label replaces the time range display */
  fixedLabel?: string
  className?: string
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function toDatetimeLocal(unix: number): string {
  const d = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function fromDatetimeLocal(s: string): number {
  return Math.floor(new Date(s).getTime() / 1000)
}

// ─── Time Range Popover Content ───────────────────────────────────────────────

interface TimePickerPopoverContentProps {
  value: TimeRangeValue
  onValueChange: (v: TimeRangeValue) => void
  onClose: () => void
}

function TimePickerPopoverContent({
  value,
  onValueChange,
  onClose,
}: TimePickerPopoverContentProps) {
  const { t } = useTranslation()
  // Initialize absolute range inputs from current value
  const [absFrom, setAbsFrom] = useState<string>(() => {
    if (value.type === "absolute") return toDatetimeLocal(value.start)
    const tr = computeTimeRange(value.preset)
    return toDatetimeLocal(tr.start)
  })
  const [absTo, setAbsTo] = useState<string>(() => {
    if (value.type === "absolute") return toDatetimeLocal(value.end)
    const tr = computeTimeRange(value.preset)
    return toDatetimeLocal(tr.end)
  })

  const handleApplyAbsolute = useCallback(() => {
    if (!absFrom || !absTo) return
    const start = fromDatetimeLocal(absFrom)
    const end = fromDatetimeLocal(absTo)
    if (start >= end) return
    onValueChange({ type: "absolute", start, end })
    onClose()
  }, [absFrom, absTo, onValueChange, onClose])

  const handleSelectPreset = useCallback(
    (preset: TimeRangePreset) => {
      // Sync absolute inputs to the computed range for this preset
      const tr = computeTimeRange(preset)
      setAbsFrom(toDatetimeLocal(tr.start))
      setAbsTo(toDatetimeLocal(tr.end))
      onValueChange({ type: "preset", preset })
      onClose()
    },
    [onValueChange, onClose],
  )

  const activePreset = value.type === "preset" ? value.preset : null
  const absFromValid = absFrom && absTo && fromDatetimeLocal(absFrom) < fromDatetimeLocal(absTo)

  // Get timezone name
  const timezoneName = Intl.DateTimeFormat().resolvedOptions().timeZone

  return (
    <div className="flex flex-col sm:flex-row sm:gap-0">
      {/* Left: Absolute time range */}
      <div className="flex flex-col gap-3 border-b p-4 sm:w-72 sm:border-r sm:border-b-0">
        <div>
          <p className="text-muted-foreground mb-3 font-mono text-xs font-semibold tracking-wider uppercase">
            {t("prometheus.absoluteTimeRange")}
          </p>
          <div className="flex flex-col gap-2">
            <div className="flex flex-col gap-1">
              <label className="text-muted-foreground font-mono text-xs">
                {t("prometheus.from")}
              </label>
              <input
                type="datetime-local"
                value={absFrom}
                onChange={(e) => setAbsFrom(e.target.value)}
                step="1"
                className="border-border bg-background text-foreground w-full rounded border px-2 py-1.5 font-mono text-xs focus:ring-1 focus:outline-none"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-muted-foreground font-mono text-xs">
                {t("prometheus.to")}
              </label>
              <input
                type="datetime-local"
                value={absTo}
                onChange={(e) => setAbsTo(e.target.value)}
                step="1"
                className="border-border bg-background text-foreground w-full rounded border px-2 py-1.5 font-mono text-xs focus:ring-1 focus:outline-none"
              />
            </div>
          </div>
        </div>

        <Button
          size="sm"
          className="w-full font-mono text-xs"
          onClick={handleApplyAbsolute}
          disabled={!absFromValid}
        >
          {t("prometheus.applyTimeRange")}
        </Button>

        {/* Timezone info */}
        <div className="text-muted-foreground flex items-center gap-1.5 font-mono text-xs">
          <Globe className="h-3 w-3 shrink-0" />
          <span>{t("prometheus.browserTime", { timezoneName })}</span>
        </div>
      </div>

      {/* Right: Quick ranges */}
      <div className="p-2 sm:w-48">
        <p className="text-muted-foreground mb-1 px-2 py-1 font-mono text-xs font-semibold tracking-wider uppercase">
          {t("prometheus.quickRanges")}
        </p>
        <div className="flex flex-col">
          {PRESET_ORDER.map((preset) => {
            const isActive = activePreset === preset
            return (
              <button
                key={preset}
                type="button"
                onClick={() => handleSelectPreset(preset)}
                className={cn(
                  "flex items-center gap-2 rounded px-2 py-1.5 text-left font-mono text-xs transition-colors",
                  isActive
                    ? "bg-accent text-accent-foreground font-medium"
                    : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                )}
              >
                {isActive && <Check className="h-3 w-3 shrink-0" />}
                {!isActive && <span className="h-3 w-3 shrink-0" />}
                {t(PRESET_LABEL_KEYS[preset])}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// ─── Main Component ────────────────────────────────────────────────────────────

export function GrafanaTimePicker({
  value,
  onValueChange,
  refreshInterval,
  onRefreshIntervalChange,
  onRefresh,
  countdown,
  isFetching = false,
  disabled = false,
  fixedLabel,
  className,
}: GrafanaTimePickerProps) {
  const { t } = useTranslation()
  const [pickerOpen, setPickerOpen] = useState(false)

  const formatLabel = useCallback(
    (v: TimeRangeValue): string => {
      if (v.type === "preset") return t(PRESET_LABEL_KEYS[v.preset])
      return `${formatUnixTimestamp(v.start)} ${t("prometheus.timeRangeTo")} ${formatUnixTimestamp(v.end)}`
    },
    [t],
  )

  const displayLabel = disabled ? (fixedLabel ?? formatLabel(value)) : formatLabel(value)

  const handleZoomOut = useCallback(() => {
    if (disabled) return
    onValueChange(zoomOut(value))
  }, [disabled, value, onValueChange])

  const isAtMaxZoom =
    value.type === "preset" && value.preset === PRESET_ORDER[PRESET_ORDER.length - 1]

  return (
    <div className={cn("flex min-w-0 items-center gap-1", className)}>
      {/* ── Time range picker trigger ── */}
      <Popover open={pickerOpen} onOpenChange={disabled ? undefined : setPickerOpen}>
        <PopoverTrigger
          disabled={disabled}
          className={cn(
            "border-border hover:bg-muted flex h-8 min-w-0 items-center gap-1.5 rounded-md border px-2.5 font-mono text-xs transition-colors",
            disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
          )}
        >
          <Clock className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
          <span
            className={cn(
              "min-w-0 flex-1 whitespace-nowrap",
              disabled ? "text-muted-foreground" : "text-foreground",
            )}
          >
            {displayLabel}
          </span>
          <ChevronDown
            className={cn(
              "h-3 w-3 shrink-0 transition-transform",
              pickerOpen && "rotate-180",
              "text-muted-foreground",
            )}
          />
        </PopoverTrigger>

        <PopoverContent className="w-auto p-0" align="start" sideOffset={4}>
          <TimePickerPopoverContent
            value={value}
            onValueChange={onValueChange}
            onClose={() => setPickerOpen(false)}
          />
        </PopoverContent>
      </Popover>

      {/* ── Zoom Out button ── */}
      <Button
        variant="outline"
        size="sm"
        className={cn("h-8 w-8 p-0", (disabled || isAtMaxZoom) && "cursor-not-allowed opacity-50")}
        onClick={handleZoomOut}
        disabled={disabled || isAtMaxZoom}
        title={t("prometheus.zoomOut")}
      >
        <ZoomOut className="h-3.5 w-3.5" />
      </Button>

      {/* ── Refresh button + interval picker ── */}
      <div
        className={cn(
          "border-border flex h-8 items-stretch rounded-md border",
          disabled && "opacity-50",
        )}
      >
        {/* Main refresh button with countdown */}
        <button
          type="button"
          onClick={disabled ? undefined : onRefresh}
          disabled={disabled}
          title={t("prometheus.refreshNow")}
          className={cn(
            "flex items-center gap-1.5 px-2.5 font-mono text-xs transition-colors",
            disabled ? "cursor-not-allowed" : "hover:bg-muted cursor-pointer",
            "rounded-l-md",
          )}
        >
          <RefreshCw
            className={cn(
              "h-3.5 w-3.5",
              !disabled && "group-hover:text-foreground",
              isFetching && "animate-spin",
            )}
          />
          {countdown !== null && countdown !== undefined && (
            <span className="text-muted-foreground tabular-nums">{countdown}s</span>
          )}
        </button>

        {/* Divider */}
        <div className="border-border w-px self-stretch border-l" />

        {/* Interval dropdown trigger */}
        <DropdownMenu>
          <DropdownMenuTrigger
            disabled={disabled}
            className={cn(
              "flex h-full items-center rounded-r-md px-1.5 transition-colors",
              disabled ? "cursor-not-allowed" : "hover:bg-muted cursor-pointer",
            )}
          >
            <ChevronDown className="text-muted-foreground h-3 w-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="bottom" sideOffset={4}>
            {REFRESH_INTERVALS.map((interval) => (
              <DropdownMenuItem
                key={interval}
                onClick={() => onRefreshIntervalChange(interval)}
                className={cn(
                  "flex items-center gap-2 font-mono text-xs",
                  refreshInterval === interval && "font-semibold",
                )}
              >
                {refreshInterval === interval && <Check className="h-3 w-3 shrink-0" />}
                {refreshInterval !== interval && <span className="h-3 w-3 shrink-0" />}
                {REFRESH_INTERVAL_LABELS[interval]}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )
}
