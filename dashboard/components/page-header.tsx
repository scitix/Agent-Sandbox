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

import { useEffect } from "react"
import React from "react"
import Link from "next/link"
import { useAtomValue, useSetAtom } from "jotai"
import { PanelLeft } from "lucide-react"
import { useQuery } from "@tanstack/react-query"
import { LiveBadge } from "@/components/live-badge"
import { ClusterSwitcher } from "@/components/cluster-switcher"
import { Button } from "@/components/ui/button"
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { useSidebar } from "@/components/ui/sidebar"
import { useBreadcrumbs } from "@/hooks/use-breadcrumbs"
import { userSandboxStatsQueryOptions } from "@/lib/queries"
import { concurrentSandboxesAtom, authAtom } from "@/lib/atoms"

/**
 * The single, route-driven header rendered once by the cluster layout. The left
 * side is a breadcrumb whose trailing crumb doubles as the page title; the right
 * side carries the cluster switcher and the live concurrent-sandbox badge.
 *
 * It also owns the running-count poll that feeds `concurrentSandboxesAtom` (read
 * by LiveBadge) — mounting it here keeps that count fresh on every page.
 */
export function PageHeader() {
  const auth = useAtomValue(authAtom)
  const setRunningCount = useSetAtom(concurrentSandboxesAtom)
  const { isMobile, toggleSidebar } = useSidebar()
  const crumbs = useBreadcrumbs()

  const { data: statsData } = useQuery({
    ...userSandboxStatsQueryOptions(),
    refetchInterval: auth ? 30000 : false,
    enabled: !!auth,
  })

  useEffect(() => {
    const stats = statsData as { statistics?: { byStatus?: Record<string, number> } } | undefined
    setRunningCount(stats?.statistics?.byStatus?.["Running"] ?? 0)
  }, [statsData, setRunningCount])

  return (
    <div className="border-border flex h-13 shrink-0 items-center justify-between border-b px-6">
      <div className="flex min-w-0 items-center gap-2">
        {isMobile && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={toggleSidebar}
            className="text-muted-foreground hover:text-foreground -ml-2"
            aria-label="Open sidebar"
          >
            <PanelLeft className="h-5 w-5" />
          </Button>
        )}
        <Breadcrumb>
          <BreadcrumbList>
            {crumbs.map((crumb, i) => (
              <React.Fragment key={`${crumb.label}-${i}`}>
                {i > 0 && <BreadcrumbSeparator />}
                <BreadcrumbItem className="min-w-0">
                  {crumb.isCurrent || !crumb.href ? (
                    <BreadcrumbPage className="truncate font-mono font-bold tracking-tight uppercase select-none">
                      {crumb.label}
                    </BreadcrumbPage>
                  ) : (
                    <BreadcrumbLink
                      render={<Link href={crumb.href} />}
                      className="font-mono tracking-tight uppercase select-none"
                    >
                      {crumb.label}
                    </BreadcrumbLink>
                  )}
                </BreadcrumbItem>
              </React.Fragment>
            ))}
          </BreadcrumbList>
        </Breadcrumb>
      </div>
      <div className="flex shrink-0 items-center gap-3" hidden={isMobile}>
        <ClusterSwitcher compact />
        <LiveBadge />
      </div>
    </div>
  )
}
