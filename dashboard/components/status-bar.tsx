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

import { useAtomValue } from "jotai"
import { systemStatusAtom, authAtom } from "@/lib/atoms"
import { usePathname } from "next/navigation"
import { toast } from "sonner"

export function StatusBar() {
  const status = useAtomValue(systemStatusAtom)
  const auth = useAtomValue(authAtom)
  const pathname = usePathname()

  const displayUser = auth?.user ?? "anonymous"
  const displayTeam = auth?.team ?? "anonymous"
  const currentSection = pathname.split("/").filter(Boolean).pop()?.toUpperCase() || "DASHBOARD"

  return (
    <div className="border-border bg-card text-muted-foreground flex h-8 items-center justify-between border-t px-4 font-mono text-xs">
      <div className="flex items-center gap-4">
        <button
          className="hover:text-foreground transition-colors"
          onClick={() => toast.warning("WIP")}
        >
          Feedback
        </button>
        <button
          className="hover:text-foreground transition-colors"
          onClick={() => toast.warning("WIP")}
        >
          Report Issue
        </button>
      </div>
      <div className="flex items-center gap-4">
        <span className="text-muted-foreground/70 hidden sm:inline">
          {">"}_{displayUser.toUpperCase()}:{displayTeam.toUpperCase()}:{currentSection}
        </span>
      </div>
      <div className="flex items-center gap-2">
        {status === "operational" && (
          <>
            <span className="bg-success h-2 w-2" />
            <span className="tracking-wide uppercase">All Systems Operational</span>
          </>
        )}
        {status === "degraded" && (
          <>
            <span className="bg-brand h-2 w-2" />
            <span className="tracking-wide uppercase">Degraded Performance</span>
          </>
        )}
        {status === "down" && (
          <>
            <span className="bg-destructive h-2 w-2" />
            <span className="tracking-wide uppercase">Systems Down</span>
          </>
        )}
      </div>
    </div>
  )
}
