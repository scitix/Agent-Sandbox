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
import { useAtomValue, useSetAtom } from "jotai"
import { LiveBadge } from "@/components/live-badge"
import { PanelLeft } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useSidebar } from "@/components/ui/sidebar"
import { useQuery } from "@tanstack/react-query"
import { userSandboxStatsQueryOptions } from "@/lib/queries"
import { concurrentSandboxesAtom, authAtom } from "@/lib/atoms"

interface PageHeaderProps {
  title: string
  children?: React.ReactNode
}

export function PageHeader({ title, children }: PageHeaderProps) {
  const auth = useAtomValue(authAtom)
  const setRunningCount = useSetAtom(concurrentSandboxesAtom)
  const { isMobile, toggleSidebar } = useSidebar()

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
    <div className="border-border flex flex-col border-b">
      <div className="flex items-center justify-between px-6 pt-5 pb-0">
        <div className="flex items-center gap-1">
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
          <h1 className="font-mono text-xl font-bold tracking-tight uppercase">{title}</h1>
        </div>
        <div className="flex items-center gap-3" hidden={isMobile}>
          <LiveBadge />
        </div>
      </div>
      {children ? (
        <div className="flex items-center gap-4 px-6">{children}</div>
      ) : (
        <div className="pb-5" />
      )}
    </div>
  )
}
