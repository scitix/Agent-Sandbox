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
 * DetailTabsNav — sub-route-driven underline tab bar for resource detail pages.
 *
 * Unlike a Base UI Tabs widget, each tab is a real <Link> to a child route
 * (e.g. `…/envs/{name}/pools`). The detail header lives in the shared layout
 * above this bar; the child page renders below it. The active tab is derived
 * from the current pathname, so deep links and refreshes land on the right
 * sub-page and a deeper route (e.g. `…/pools/{poolName}`) keeps its parent tab
 * highlighted.
 */

import Link from "next/link"
import { usePathname } from "next/navigation"
import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

export interface DetailNavTab {
  /**
   * Path segment appended to basePath (also the route folder name). An empty
   * string marks the index tab, whose route is basePath itself — it highlights
   * only on an exact match, so deeper tabs don't light it up too.
   */
  value: string
  label: string
  icon?: LucideIcon
}

export interface DetailTabsNavProps {
  /** Route prefix the tabs hang off, e.g. `/clusters/{id}/envs/{name}`. */
  basePath: string
  tabs: DetailNavTab[]
}

// Underline tab styling, mirroring the look of components/custom/detail-tabs.tsx
// but applied to anchors instead of Base UI tab triggers.
const TAB_CN = cn(
  "inline-flex h-9 flex-none items-center gap-1.5 border-b-2 pt-2 pb-3",
  "text-sm font-medium whitespace-nowrap transition-colors",
  "focus-visible:ring-0 focus-visible:outline-none",
  "[&_svg]:pointer-events-none [&_svg]:shrink-0",
)
const TAB_ACTIVE_CN = "border-b-brand text-foreground"
const TAB_INACTIVE_CN = "border-b-transparent text-muted-foreground hover:text-foreground"

export function DetailTabsNav({ basePath, tabs }: DetailTabsNavProps) {
  const pathname = usePathname()

  return (
    <div className="border-border shrink-0 border-b px-6">
      <nav className="flex h-9 gap-6">
        {tabs.map((tab) => {
          const href = tab.value ? `${basePath}/${tab.value}` : basePath
          const active = tab.value
            ? pathname === href || pathname.startsWith(`${href}/`)
            : pathname === basePath
          return (
            <Link
              key={tab.value || "index"}
              href={href}
              className={cn(TAB_CN, active ? TAB_ACTIVE_CN : TAB_INACTIVE_CN)}
            >
              {tab.icon && <tab.icon className="h-3.5 w-3.5" />}
              {tab.label}
            </Link>
          )
        })}
      </nav>
    </div>
  )
}
