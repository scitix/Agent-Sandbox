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

import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card"
import { cn } from "@/lib/utils"

/**
 * Color class map: keys are lowercased status strings → Tailwind class string.
 * Example:
 *   { running: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30" }
 */
export type StatusBadgeColorMap = Record<string, string>

interface StatusBadgeProps {
  /** The status key used for color lookup (lowercased against colorMap). */
  status: string | undefined | null
  /** Override the display label. When omitted, `status` is shown instead. */
  label?: string
  /** Map of lowercase status → Tailwind classes for background/text/border. */
  colorMap: StatusBadgeColorMap
  /** Fallback class when status is absent or not found in colorMap. */
  defaultClass?: string
  /** When provided, wraps the badge in a HoverCard showing this content. */
  hoverContent?: React.ReactNode
  /** Extra class names forwarded to the Badge element. */
  className?: string
}

/**
 * Generic status badge component with optional HoverCard.
 *
 * Usage:
 *   <StatusBadge status="Ready" colorMap={POOL_PHASE_COLORS} hoverContent={<p>details</p>} />
 */
export function StatusBadge({
  status,
  label,
  colorMap,
  defaultClass = "",
  hoverContent,
  className,
}: StatusBadgeProps) {
  const displayLabel = label ?? status ?? "---"
  const colorClass = status
    ? (colorMap[status.toLowerCase()] ?? colorMap[status] ?? defaultClass)
    : defaultClass

  const badge = (
    <Badge
      variant="outline"
      className={cn(
        "font-mono text-xs uppercase",
        colorClass,
        { "cursor-help": !!hoverContent },
        className,
      )}
    >
      {displayLabel}
    </Badge>
  )

  if (!hoverContent) return badge

  return (
    <HoverCard>
      <HoverCardTrigger className="cursor-default">{badge}</HoverCardTrigger>
      <HoverCardContent className="w-80 p-3 text-xs">{hoverContent}</HoverCardContent>
    </HoverCard>
  )
}
