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
