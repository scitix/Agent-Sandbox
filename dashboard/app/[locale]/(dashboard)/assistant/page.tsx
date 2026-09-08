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
 * The cluster-less entry point.
 *
 * Redirects into the current cluster's assistant, so a bookmark, the sidebar of
 * a page that has no cluster, and the landing redirect all arrive somewhere the
 * agent can be told which cluster is meant.
 */

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { authAtom } from "@/lib/atoms"
import { clusterPath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"
import { Spinner } from "@/components/ui/spinner"

export default function AssistantRedirectPage() {
  const auth = useAtomValue(authAtom)
  const router = useRouter()
  const locale = useLocale()

  useEffect(() => {
    if (!auth) return // the layout's AuthGuard sends this to /login
    router.replace(
      clusterPath(auth.clusterID ?? "default", "assistant", locale)
    )
  }, [auth, router, locale])

  return (
    <div className="flex min-h-0 flex-1 items-center justify-center">
      <Spinner className="size-4" />
    </div>
  )
}
