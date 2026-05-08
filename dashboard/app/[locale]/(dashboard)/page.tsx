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

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { authAtom, isAdminAtom } from "@/lib/atoms"
import { clusterPath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"

export default function DashboardHomePage() {
  const auth = useAtomValue(authAtom)
  const isAdmin = useAtomValue(isAdminAtom)
  const router = useRouter()
  const locale = useLocale()
  const [hydrated, setHydrated] = useState(false)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  useEffect(() => {
    if (!hydrated) return
    if (!auth) return // AuthGuard in layout will handle redirect to /login
    const clusterID = auth.clusterID ?? "default"
    if (isAdmin) {
      router.replace(clusterPath(clusterID, "sandboxes", locale))
    } else {
      router.replace(clusterPath(clusterID, "overview", locale))
    }
  }, [hydrated, auth, router, isAdmin])

  return (
    <div className="bg-background flex min-h-screen items-center justify-center">
      <div className="flex flex-col items-center gap-2">
        <div className="bg-brand h-1 w-24 animate-pulse" />
        <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
          Loading...
        </span>
      </div>
    </div>
  )
}
