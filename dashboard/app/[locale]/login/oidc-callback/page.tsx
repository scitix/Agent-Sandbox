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

import { useEffect, Suspense } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { useSetAtom } from "jotai"
import { authAtom } from "@/lib/atoms"
import type { AuthState } from "@/lib/atoms"
import { Loader2 } from "lucide-react"
import { clusterPath, loginPath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"

/**
 * /[locale]/login/oidc-callback
 *
 * Receives the JWT and auth state from the BFF (/api/auth/oidc/callback) via
 * URL search params, writes them to the authAtom (persisted in localStorage),
 * then redirects to the appropriate dashboard page.
 *
 * The token is never stored in the address bar after the redirect.
 */
function OIDCCallbackInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const setAuth = useSetAtom(authAtom)
  const locale = useLocale()

  useEffect(() => {
    const token = searchParams.get("token")
    const role = searchParams.get("role") as AuthState["role"] | null
    const user = searchParams.get("user") ?? undefined
    const team = searchParams.get("team") ?? undefined
    const clusterID = searchParams.get("clusterID") ?? undefined
    const clusterName = searchParams.get("clusterName") ?? undefined
    const name = searchParams.get("name") ?? undefined
    const email = searchParams.get("email") ?? undefined

    if (!token || !role) {
      router.replace(`${loginPath(locale)}?error=oidc_failed`)
      return
    }

    const authState: AuthState = {
      token,
      role,
      user,
      team,
      clusterID,
      clusterName,
      authMethod: "oidc",
      name,
      email,
    }

    setAuth(authState)

    const redirectTo = searchParams.get("redirect")
    if (redirectTo && redirectTo.startsWith("/")) {
      router.replace(redirectTo)
      return
    }

    const resolvedClusterID = clusterID ?? "default"
    if (role === "admin") {
      router.replace(clusterPath(resolvedClusterID, "sandboxes", locale))
    } else {
      router.replace(clusterPath(resolvedClusterID, "overview", locale))
    }
  }, [searchParams, router, setAuth, locale])

  return (
    <div className="bg-background flex min-h-screen items-center justify-center">
      <div className="flex flex-col items-center gap-3">
        <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
        <p className="text-muted-foreground font-mono text-xs">Signing you in…</p>
      </div>
    </div>
  )
}

export default function OIDCCallbackPage() {
  return (
    <Suspense
      fallback={
        <div className="bg-background flex min-h-screen items-center justify-center">
          <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
        </div>
      }
    >
      <OIDCCallbackInner />
    </Suspense>
  )
}
