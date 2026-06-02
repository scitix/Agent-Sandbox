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

import { use } from "react"
import { useQuery } from "@tanstack/react-query"

import { PoolMetricsPanel } from "@/components/prometheus/pool-metrics-panel"
import { envPoolQueryOptions } from "@/lib/queries"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; poolName: string; locale: string }>
}

/** Metrics tab — replica trend + schedule queue charts for the pool. */
export default function PoolMetricsPage({ params }: PageProps) {
  const { name, poolName } = use(params)
  const { data } = useQuery(envPoolQueryOptions(name, poolName))
  const pool = data?.template ?? null

  if (!pool) return null

  // key forces a fresh panel (and a re-seeded default time range) per pool.
  return <PoolMetricsPanel key={pool.name} pool={pool} />
}
