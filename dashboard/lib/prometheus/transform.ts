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

/**
 * Prometheus Data Transform Utilities
 *
 * Converts BFF response data (ChartSeries[]) into formats suitable for Recharts.
 */

import type { ChartSeries } from "@/lib/types/prometheus"

/** A Recharts-compatible data point where each series is a named key */
export type RechartsDataPoint = Record<string, number | string>

/**
 * Merge multiple ChartSeries into a flat array of Recharts data points.
 *
 * Each output object has:
 *   - `time`: millisecond timestamp (number, for XAxis dataKey)
 *   - `timeLabel`: ISO string (string, for tooltip display)
 *   - one key per series name with its value at that timestamp
 *
 * Missing values at a timestamp are filled with 0.
 */
export function mergeChartSeries(series: ChartSeries[]): RechartsDataPoint[] {
  if (series.length === 0) return []

  // Collect all unique timestamps across all series
  const allTimestamps = [...new Set(series.flatMap((s) => s.points.map((p) => p.time)))].sort(
    (a, b) => a - b,
  )

  return allTimestamps.map((ts) => {
    const point: RechartsDataPoint = {
      time: ts,
      timeLabel: new Date(ts).toISOString(),
    }
    for (const s of series) {
      const match = s.points.find((p) => p.time === ts)
      point[s.name] = match?.value ?? 0
    }
    return point
  })
}

/**
 * Format a byte count into a human-readable string.
 * Examples: 512 → "512 B", 1536 → "1.5 KB", 1073741824 → "1 GB"
 */
export function formatBytes(bytes: number): string {
  if (bytes === null || bytes === undefined || isNaN(bytes) || bytes === 0) {
    return "0 B"
  }
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024))
  const clamped = Math.max(0, Math.min(i, units.length - 1))
  const value = bytes / Math.pow(1024, clamped)
  return `${value % 1 === 0 ? value : value.toFixed(1)} ${units[clamped]}`
}

/**
 * Format a duration in seconds into a human-readable string.
 * Sub-10s values are shown in ms for better precision on latency charts.
 * Examples: 0.045 → "45ms", 3.2 → "3.2s", 90 → "1m 30s", 3700 → "1h 1m"
 */
export function formatDuration(seconds: number): string {
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`
  if (seconds < 60) return `${Math.round(seconds)}s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.round(seconds % 60)
  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  }
  return secs > 0 ? `${minutes}m ${secs}s` : `${minutes}m`
}

/**
 * Format a per-second rate into a human-readable string.
 * Examples: 0.0167 → "1/min", 1.5 → "1.5/s", 0.00028 → "1/hr"
 */
export function formatRate(perSec: number): string {
  if (perSec === 0) return "0/s"
  if (perSec >= 0.5) return `${perSec % 1 === 0 ? perSec : perSec.toFixed(2)}/s`
  const perMin = perSec * 60
  if (perMin >= 0.5) return `${perMin % 1 === 0 ? perMin : perMin.toFixed(1)}/min`
  const perHr = perSec * 3600
  return `${perHr % 1 === 0 ? perHr : perHr.toFixed(1)}/hr`
}

/**
 * Format a duration given in milliseconds into a human-readable string.
 * Used for Envoy latency charts where the raw unit from Prometheus is ms.
 * Examples: 45 → "45ms", 1500 → "1.5s", 90000 → "1m 30s"
 */
export function formatMilliseconds(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds % 1 === 0 ? seconds : seconds.toFixed(1)}s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.round(seconds % 60)
  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  }
  return secs > 0 ? `${minutes}m ${secs}s` : `${minutes}m`
}

export function formatCores(cores: number): string {
  if (cores === 0) return "0"
  if (cores >= 1) return `${cores % 1 === 0 ? cores : cores.toFixed(2)} cores`
  return `${Math.round(cores * 1000)}m`
}
