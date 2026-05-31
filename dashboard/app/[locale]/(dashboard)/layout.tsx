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

import { useState, useEffect } from "react"
import { useRouter } from "next/navigation"
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar"
import { AppSidebar } from "@/components/app-sidebar"
import { CommandPalette, useCommandPalette } from "@/components/command-palette"
import { ErrorReportDialog } from "@/components/error-report-dialog"
import { ChangelogDialog } from "@/components/changelog/changelog-dialog"
import { useAtomValue } from "jotai"
import { authAtom } from "@/lib/atoms"
import { loginPath } from "@/lib/cluster-path"
import { basePath } from "@/lib/api/client"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const auth = useAtomValue(authAtom)
  const locale = useLocale()
  const { t } = useTranslation()
  const [hydrated, setHydrated] = useState(false)

  // Mark hydration complete after first client-side render
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  useEffect(() => {
    if (!hydrated) return // Wait for hydration before checking auth
    if (auth === null) {
      const fullPath = window.location.pathname + window.location.search
      const appPath =
        basePath && fullPath.startsWith(basePath) ? fullPath.slice(basePath.length) : fullPath
      router.replace(`${loginPath(locale)}?redirect=${encodeURIComponent(appPath)}`)
    }
  }, [hydrated, auth, router, locale])

  // Show loading spinner until hydration completes or auth is confirmed
  if (!hydrated || auth === null) {
    return (
      <div className="bg-background flex min-h-screen items-center justify-center">
        <div className="flex flex-col items-center gap-2">
          <div className="bg-brand h-1 w-24 animate-pulse" />
          <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
            {t("auth.authenticating")}
          </span>
        </div>
      </div>
    )
  }

  return <>{children}</>
}

function DashboardShell({ children }: { children: React.ReactNode }) {
  const { open, setOpen } = useCommandPalette()

  return (
    <SidebarProvider>
      <AppSidebar onOpenCommand={() => setOpen(true)} />
      <SidebarInset className="relative flex min-h-svh w-full flex-1 flex-col">
        <main className="@container/main absolute inset-0 flex flex-col overflow-hidden p-0">
          {children}
        </main>
      </SidebarInset>
      <CommandPalette open={open} onOpenChange={setOpen} />
    </SidebarProvider>
  )
}

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGuard>
      <DashboardShell>{children}</DashboardShell>
      <ErrorReportDialog />
      <ChangelogDialog />
    </AuthGuard>
  )
}
