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

// TopUsersBarChart — horizontal bar chart of the top-N users by sandbox
// creation count, with the remainder merged into a single "Other" bar.

import { useMemo } from "react"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell } from "recharts"
import { Loader2 } from "lucide-react"
import { Card } from "@/components/ui/card"
import { C } from "./colors"

export interface UserCountRow {
  team: string
  user: string
  count: number
}

interface TopUsersBarChartProps {
  title: string
  rows: UserCountRow[]
  topN?: number
  isLoading?: boolean
  emptyLabel?: string
  otherLabel: string
  /** Renders without the Card wrapper, for embedding in a card that owns its own header. */
  bare?: boolean
}

export function TopUsersBarChart({
  title,
  rows,
  topN = 20,
  isLoading,
  emptyLabel,
  otherLabel,
  bare = false,
}: TopUsersBarChartProps) {
  const chartData = useMemo(() => {
    const sorted = [...rows].sort((a, b) => b.count - a.count)
    const top = sorted.slice(0, topN)
    const rest = sorted.slice(topN)
    const restTotal = rest.reduce((sum, r) => sum + r.count, 0)
    const data = top.map((r) => ({ label: `${r.team}/${r.user}`, count: r.count }))
    if (restTotal > 0) data.push({ label: otherLabel, count: restTotal })
    // Recharts lays vertical bars out top-to-bottom in array order, so
    // descending order already puts the biggest contributor at the top.
    return data
  }, [rows, topN, otherLabel])

  const total = useMemo(() => rows.reduce((sum, r) => sum + r.count, 0), [rows])
  const height = Math.max(240, chartData.length * 28)

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
        <div style={{ height }}>
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={chartData} layout="vertical" margin={{ left: 8, right: 16 }}>
              <CartesianGrid strokeDasharray="3 3" horizontal={false} />
              <XAxis type="number" tick={{ fontSize: 11 }} />
              <YAxis
                type="category"
                dataKey="label"
                width={160}
                tick={{ fontSize: 11, fontFamily: "var(--font-mono)" }}
              />
              <Tooltip formatter={(value) => Number(value).toLocaleString()} />
              <Bar dataKey="count" radius={[0, 4, 4, 0]}>
                {chartData.map((entry, i) => (
                  <Cell
                    key={entry.label}
                    // The top contributor carries the brand colour — it is the
                    // one number a reader takes away from this chart.
                    fill={
                      entry.label === otherLabel
                        ? C.idle
                        : i === 0
                          ? "var(--brand)"
                          : C.desired
                    }
                  />
                ))}
              </Bar>
            </BarChart>
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
