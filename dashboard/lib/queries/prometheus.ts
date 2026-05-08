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
 * Prometheus React Query Hooks
 *
 * All hooks call the typed BFF endpoints (no raw PromQL from the client).
 * When a hook returns `data.configured === false`, the calling component
 * should hide itself entirely — do NOT show error states.
 *
 * staleTime is set to 30s (Prometheus default scrape interval is ~15s).
 */

import { queryOptions, useQuery, keepPreviousData } from "@tanstack/react-query"
import { prometheusGet } from "@/lib/prometheus/client"
import type {
  ReplicasOverviewData,
  StartRateData,
  PeakConcurrentData,
  TimeSeriesData,
  UserSummaryData,
  SandboxCumulativeStatsData,
  SandboxUserStatsData,
  EnvoyBandwidthData,
  SandboxFilters,
  TimeRangePreset,
} from "@/lib/types/prometheus"

const STALE_TIME = 30_000 // 30 seconds

// ─── Query key helpers ─────────────────────────────────────────────────────

function filterKey(f: SandboxFilters) {
  return [f.cluster, f.team ?? "", f.user ?? "", f.pool ?? ""]
}

// ─── queryOptions factories ────────────────────────────────────────────────

export const replicasOverviewQueryOptions = (
  filters: SandboxFilters,
  options?: { refetchInterval?: number; endTime?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "replicas-overview", ...filterKey(filters), options?.endTime ?? "now"],
    queryFn: () =>
      prometheusGet<ReplicasOverviewData>("replicas-overview", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        ...(options?.endTime ? { end: options.endTime } : {}),
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const startRateQueryOptions = (
  filters: SandboxFilters,
  options?: { refetchInterval?: number; endTime?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "start-rate", ...filterKey(filters), options?.endTime ?? "now"],
    queryFn: () =>
      prometheusGet<StartRateData>("start-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        ...(options?.endTime ? { end: options.endTime } : {}),
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const peakConcurrentQueryOptions = (
  filters: SandboxFilters,
  lookbackHours = 1,
  options?: { refetchInterval?: number; endTime?: number },
) =>
  queryOptions({
    queryKey: [
      "prometheus",
      "peak-concurrent",
      ...filterKey(filters),
      lookbackHours,
      options?.endTime ?? "now",
    ],
    queryFn: () =>
      prometheusGet<PeakConcurrentData>("peak-concurrent", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        lookbackHours,
        ...(options?.endTime ? { end: options.endTime } : {}),
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const replicasTrendQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "replicas-trend", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("replicas-trend", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const replicasTrendAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "replicas-trend", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("replicas-trend", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const claimDurationQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "claim-duration", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("claim-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const claimDurationAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "claim-duration", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("claim-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const runningDurationQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "running-duration", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("running-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const runningDurationAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "running-duration", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("running-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const recycleDurationQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "recycle-duration", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("recycle-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const recycleDurationAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "recycle-duration", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("recycle-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Pod resource queryOptions ─────────────────────────────────────────────

/** Params for pod CPU / memory queries. All callers use sandboxId — pod name stays server-side. */
export interface PodResourceParams {
  sandboxId: string
  cluster: string
  start: number
  end: number
}

export const podCpuQueryOptions = (
  params: PodResourceParams,
  options?: { enabled?: boolean; refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "pod-cpu", params.sandboxId, params.cluster, params.start, params.end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("pod-cpu", {
        sandboxId: params.sandboxId,
        cluster: params.cluster,
        start: params.start,
        end: params.end,
      }),
    enabled:
      options?.enabled ??
      (!!params.sandboxId && !!params.cluster && !!params.start && !!params.end),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const podMemoryQueryOptions = (
  params: PodResourceParams,
  options?: { enabled?: boolean; refetchInterval?: number },
) =>
  queryOptions({
    queryKey: [
      "prometheus",
      "pod-memory",
      params.sandboxId,
      params.cluster,
      params.start,
      params.end,
    ],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("pod-memory", {
        sandboxId: params.sandboxId,
        cluster: params.cluster,
        start: params.start,
        end: params.end,
      }),
    enabled:
      options?.enabled ??
      (!!params.sandboxId && !!params.cluster && !!params.start && !!params.end),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const podNetworkQueryOptions = (
  params: PodResourceParams,
  options?: { enabled?: boolean; refetchInterval?: number },
) =>
  queryOptions({
    queryKey: [
      "prometheus",
      "pod-network",
      params.sandboxId,
      params.cluster,
      params.start,
      params.end,
    ],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("pod-network", {
        sandboxId: params.sandboxId,
        cluster: params.cluster,
        start: params.start,
        end: params.end,
      }),
    enabled:
      options?.enabled ??
      (!!params.sandboxId && !!params.cluster && !!params.start && !!params.end),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const podDiskIoQueryOptions = (
  params: PodResourceParams,
  options?: { enabled?: boolean; refetchInterval?: number },
) =>
  queryOptions({
    queryKey: [
      "prometheus",
      "pod-disk-io",
      params.sandboxId,
      params.cluster,
      params.start,
      params.end,
    ],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("pod-disk-io", {
        sandboxId: params.sandboxId,
        cluster: params.cluster,
        start: params.start,
        end: params.end,
      }),
    enabled:
      options?.enabled ??
      (!!params.sandboxId && !!params.cluster && !!params.start && !!params.end),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Pre-built hooks ───────────────────────────────────────────────────────

/** Current replica counts for all pools. Auto-refetches at given interval (default: off). */
export function useReplicasOverview(
  filters: SandboxFilters,
  refetchInterval?: number,
  endTime?: number,
) {
  return useQuery(
    replicasOverviewQueryOptions(filters, {
      refetchInterval: refetchInterval || undefined,
      endTime,
    }),
  )
}

/** Sandbox creation rate per second (5m window). Auto-refetches at given interval (default: off). */
export function useStartRate(filters: SandboxFilters, refetchInterval?: number, endTime?: number) {
  return useQuery(
    startRateQueryOptions(filters, { refetchInterval: refetchInterval || undefined, endTime }),
  )
}

/** Peak concurrent sandboxes over a lookback window. */
export function usePeakConcurrent(
  filters: SandboxFilters,
  lookbackHours = 1,
  refetchInterval?: number,
  endTime?: number,
) {
  return useQuery(
    peakConcurrentQueryOptions(filters, lookbackHours, {
      refetchInterval: refetchInterval || undefined,
      endTime,
    }),
  )
}

/** Desired vs Running replica trend over time. */
export function useReplicasTrend(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    replicasTrendQueryOptions(filters, preset, { refetchInterval: refetchInterval || undefined }),
  )
}

/** ClaimIdlePod latency percentiles over time. */
export function useClaimDurationPercentiles(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    claimDurationQueryOptions(filters, preset, { refetchInterval: refetchInterval || undefined }),
  )
}

/** Sandbox running duration percentiles over time. */
export function useRunningDurationPercentiles(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    runningDurationQueryOptions(filters, preset, { refetchInterval: refetchInterval || undefined }),
  )
}

/** Pod recycle (Stopping→Idle) latency percentiles over time. */
export function useRecycleDurationPercentiles(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    recycleDurationQueryOptions(filters, preset, { refetchInterval: refetchInterval || undefined }),
  )
}

/** CPU usage for a sandbox over a time range. */
export function usePodCpu(params: PodResourceParams, refetchInterval?: number) {
  return useQuery(podCpuQueryOptions(params, { enabled: true, refetchInterval }))
}

/** Memory working set for a sandbox over a time range. */
export function usePodMemory(params: PodResourceParams, refetchInterval?: number) {
  return useQuery(podMemoryQueryOptions(params, { enabled: true, refetchInterval }))
}

/** Network RX + TX bandwidth for a sandbox over a time range. */
export function usePodNetwork(params: PodResourceParams, refetchInterval?: number) {
  return useQuery(podNetworkQueryOptions(params, { enabled: true, refetchInterval }))
}

/** Disk read + write I/O rates for a sandbox over a time range. */
export function usePodDiskIo(params: PodResourceParams, refetchInterval?: number) {
  return useQuery(podDiskIoQueryOptions(params, { enabled: true, refetchInterval }))
}

// ─── User summary ──────────────────────────────────────────────────────────

export const userSummaryQueryOptions = (
  filters: SandboxFilters,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "user-summary", ...filterKey(filters)],
    queryFn: () =>
      prometheusGet<UserSummaryData>("user-summary", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
      }),
    staleTime: STALE_TIME,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

/** Running sandbox counts grouped by team / user. Auto-refetches at given interval (default: off). */
export function useUserSummary(filters: SandboxFilters, refetchInterval?: number) {
  return useQuery(
    userSummaryQueryOptions(filters, { refetchInterval: refetchInterval || undefined }),
  )
}

// ─── Sandbox create rate (all users) ──────────────────────────────────────

export const sandboxCreateRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-create-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-create-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxCreateRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-create-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-create-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Sandbox delete rate (all users) ──────────────────────────────────────

export const sandboxDeleteRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-delete-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-delete-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxDeleteRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-delete-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-delete-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Sandbox rate trend (combined create+delete, legacy) ───────

export const sandboxRateTrendQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-rate-trend", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-rate-trend", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxRateTrendAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-rate-trend", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-rate-trend", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: HTTP request rate ────────────────────────────────────────

export const httpRequestRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const httpRequestRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: HTTP request duration — Native API ───────────────────────

export const httpRequestDurationNativeQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-duration-native", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-duration-native", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const httpRequestDurationNativeAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-duration-native", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-duration-native", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: HTTP request duration — E2B API ──────────────────────────

export const httpRequestDurationE2bQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-duration-e2b", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-duration-e2b", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const httpRequestDurationE2bAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "http-request-duration-e2b", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("http-request-duration-e2b", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Sandbox cumulative stats ─────────────────────────────────────

export const sandboxCumulativeStatsQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-cumulative-stats", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<SandboxCumulativeStatsData>("sandbox-cumulative-stats", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxCumulativeStatsAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-cumulative-stats", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<SandboxCumulativeStatsData>("sandbox-cumulative-stats", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

/** Cumulative sandbox create/delete counts and API request totals. Admin only. */
export function useSandboxCumulativeStats(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    sandboxCumulativeStatsQueryOptions(filters, preset, {
      refetchInterval: refetchInterval || undefined,
    }),
  )
}

// ─── Admin-only: Envoy upstream rate ──────────────────────────────────────────

export const envoyUpstreamRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-upstream-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-upstream-rate", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const envoyUpstreamRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-upstream-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-upstream-rate", {
        cluster: filters.cluster,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Envoy error rate ─────────────────────────────────────────────

export const envoyErrorRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-error-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-error-rate", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const envoyErrorRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-error-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-error-rate", {
        cluster: filters.cluster,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Envoy latency ────────────────────────────────────────────────

export const envoyLatencyQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-latency", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-latency", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const envoyLatencyAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-latency", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-latency", {
        cluster: filters.cluster,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Sandbox create error rate ────────────────────────────────────

export const sandboxCreateErrorRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-create-error-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-create-error-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxCreateErrorRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-create-error-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-create-error-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Sandbox delete fail rate ─────────────────────────────────────

export const sandboxDeleteFailRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-delete-fail-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-delete-fail-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxDeleteFailRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-delete-fail-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("sandbox-delete-fail-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Sandbox user stats (all authenticated users) ─────────────────────────

export const sandboxUserStatsQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-user-stats", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<SandboxUserStatsData>("sandbox-user-stats", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const sandboxUserStatsAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "sandbox-user-stats", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<SandboxUserStatsData>("sandbox-user-stats", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

/** Sandbox lifecycle stats for the current user. */
export function useSandboxUserStats(
  filters: SandboxFilters,
  preset: TimeRangePreset,
  refetchInterval?: number,
) {
  return useQuery(
    sandboxUserStatsQueryOptions(filters, preset, {
      refetchInterval: refetchInterval || undefined,
    }),
  )
}

// ─── Starting duration percentiles ────────────────────────────────────────

export const startingDurationQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "starting-duration", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("starting-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const startingDurationAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "starting-duration", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("starting-duration", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Sandbox schedule success rate (target=running) ───────────────────────

export const scheduleSuccessRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-success-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-success-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleSuccessRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-success-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-success-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Sandbox recycle success rate (target=idle) ───────────────────────────

export const recycleSuccessRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "recycle-success-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("recycle-success-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const recycleSuccessRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "recycle-success-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("recycle-success-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Scheduler dispatch latency (all users) ───────────────────────────────

export const scheduleDispatchLatencyQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-dispatch-latency", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-dispatch-latency", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleDispatchLatencyAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-dispatch-latency", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-dispatch-latency", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Scheduler ready queue + reservations size (all users) ────────────────

export const scheduleReadyQueueQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-ready-queue", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-ready-queue", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleReadyQueueAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-ready-queue", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-ready-queue", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Scheduler CAS outcome rate ───────────────────────────────

export const scheduleCasOutcomeQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-cas-outcome", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-cas-outcome", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleCasOutcomeAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-cas-outcome", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-cas-outcome", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Scheduler refresh rate ──────────────────────────────────

export const scheduleRefreshRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-refresh-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-refresh-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleRefreshRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-refresh-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-refresh-rate", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Scheduler internal counters ─────────────────────────────

export const scheduleInternalCountersQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-internal-counters", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-internal-counters", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const scheduleInternalCountersAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "schedule-internal-counters", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("schedule-internal-counters", {
        cluster: filters.cluster,
        team: filters.team,
        user: filters.user,
        pool: filters.pool,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Envoy bandwidth (TX/RX bytes) ────────────────────────────────

export const envoyBandwidthQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-bandwidth", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<EnvoyBandwidthData>("envoy-bandwidth", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const envoyBandwidthAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-bandwidth", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<EnvoyBandwidthData>("envoy-bandwidth", {
        cluster: filters.cluster,
        start,
        end,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

// ─── Admin-only: Envoy bandwidth rate (TX/RX bytes/s time series) ─────────────

export const envoyBandwidthRateQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-bandwidth-rate", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-bandwidth-rate", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const envoyBandwidthRateAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  step: string,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "envoy-bandwidth-rate", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<TimeSeriesData>("envoy-bandwidth-rate", {
        cluster: filters.cluster,
        start,
        end,
        step,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })
