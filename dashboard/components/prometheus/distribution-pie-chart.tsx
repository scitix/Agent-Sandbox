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

// DistributionPieChart — reusable share-of-total pie chart. Used by the
// platform-wide "overall usage" section on /overview (by-user, by-team, and
// conditionally by-cluster tabs).

import { useMemo } from "react"
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer, Legend } from "recharts"
import { Loader2 } from "lucide-react"
import { Card } from "@/components/ui/card"

export interface DistributionSlice {
  name: string
  value: number
}

// Index 0 is the brand colour: slices are sorted descending, so the largest
// share is always the one wearing it.
const PALETTE = [
  "var(--brand)",
  "#6366f1",
  "#22c55e",
  "#3b82f6",
  "#ec4899",
  "#14b8a6",
  "#a855f7",
  "#84cc16",
  "#06b6d4",
  "#eab308",
]

interface DistributionPieChartProps {
  title: string
  data: DistributionSlice[]
  isLoading?: boolean
  emptyLabel?: string
  /** Renders without the Card wrapper, for embedding in a card that owns its own header. */
  bare?: boolean
}

export function DistributionPieChart({
  title,
  data,
  isLoading,
  emptyLabel,
  bare = false,
}: DistributionPieChartProps) {
  const total = useMemo(() => data.reduce((sum, d) => sum + d.value, 0), [data])
  const slices = useMemo(() => [...data].sort((a, b) => b.value - a.value), [data])

  const body = (
    <>
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : total === 0 ? (
        <div className="text-muted-foreground flex h-64 items-center justify-center font-mono text-xs">
          {emptyLabel ?? "No data"}
        </div>
      ) : (
        <div className="h-64">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie
                data={slices}
                dataKey="value"
                nameKey="name"
                cx="50%"
                cy="50%"
                innerRadius={50}
                outerRadius={85}
                paddingAngle={1}
              >
                {slices.map((entry, i) => (
                  <Cell key={entry.name} fill={PALETTE[i % PALETTE.length]} />
                ))}
              </Pie>
              <Tooltip
                formatter={(value, name) => {
                  const n = Number(value)
                  return [`${n.toLocaleString()} (${((n / total) * 100).toFixed(1)}%)`, name]
                }}
              />
              <Legend
                verticalAlign="bottom"
                height={36}
                wrapperStyle={{ fontSize: "11px", fontFamily: "var(--font-mono)" }}
              />
            </PieChart>
          </ResponsiveContainer>
        </div>
      )}
    </>
  )

  if (bare) return body

  return (
    <Card className="p-4">
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
        {title}
      </h3>
      {body}
    </Card>
  )
}
