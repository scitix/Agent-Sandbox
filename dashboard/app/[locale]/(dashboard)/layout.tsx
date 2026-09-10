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

import { useState, useEffect, useCallback } from "react"
import { useRouter } from "next/navigation"
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar"
import { AppSidebar } from "@/components/app-sidebar"
import { CommandPalette, useCommandPalette } from "@/components/command-palette"
import {
  AssistantSidePanel,
  useAssistantPanelState,
} from "@/components/assistant-ui/assistant-side-panel"
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable"
import { useCollapsiblePanel } from "@/hooks/use-collapsible-panel"
import { usePersistedLayout } from "@/hooks/use-persisted-layout"
import { cn } from "@/lib/utils"
import type { Layout } from "react-resizable-panels"

/** Where the page/assistant split is remembered. Versioned so a future change
 *  to the panel ids does not restore a layout that no longer fits them. */
const ASSISTANT_SPLIT_KEY = "agentbox.assistant.split.v1"
import { devAutoLogin, devAutoLoginEnabled } from "@/lib/dev-auth"
import { ErrorReportDialog } from "@/components/error-report-dialog"
import { ChangelogDialog } from "@/components/changelog/changelog-dialog"
import { useAtomValue, useSetAtom } from "jotai"
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

  // Local and CI runs can sign themselves in, so a UI check does not have to
  // drive a login form to reach the pages it is checking.
  const setAuth = useSetAtom(authAtom)
  useEffect(() => {
    if (!hydrated || auth !== null || !devAutoLoginEnabled()) return
    let cancelled = false
    void devAutoLogin().then(a => {
      if (!cancelled && a) setAuth(a)
    })
    return () => {
      cancelled = true
    }
  }, [hydrated, auth, setAuth])

  useEffect(() => {
    if (!hydrated) return // Wait for hydration before checking auth
    if (auth === null) {
      // The dev sign-in is in flight; redirecting would race it.
      if (devAutoLoginEnabled()) return
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
  const { open: assistantOpen, everOpened } = useAssistantPanelState()
  const split = usePersistedLayout(ASSISTANT_SPLIT_KEY)
  // Collapsed rather than removed. Taking the panel out of the group would
  // unmount the assistant, and with it the in-memory conversation and any
  // question waiting on an answer — the whole reason it lives up here.
  const panelRef = useCollapsiblePanel(assistantOpen, everOpened)

  // Closing the column reports a layout with the assistant at zero, and saving
  // that would throw away the width the person chose the moment they shut it —
  // they would reopen to the default every time, having dragged it wider on
  // purpose. A collapse is not a width.
  const onLayoutChanged = useCallback(
    (layout: Layout) => {
      if (!layout.assistant) return
      split.onLayoutChanged(layout)
    },
    [split]
  )

  return (
    <SidebarProvider
      // Narrower than the shadcn default: at 16rem the rail took a sixth of a
      // laptop screen to show a dozen short labels, and it is the assistant
      // column beside the page that people actually want the width for.
      style={
        {
          "--sidebar-width": "14rem",
        } as React.CSSProperties
      }
    >
      <AppSidebar onOpenCommand={() => setOpen(true)} />
      <SidebarInset className="relative flex min-h-svh w-full flex-1 flex-col">
        <main className="@container/main absolute inset-0 flex flex-row overflow-hidden p-0">
          {/* One group, always here, with the page always its first panel. The
              assistant column joins as a second panel and never leaves.
              Swapping the page between a group and a bare child instead — which
              is the obvious way to write this — makes React unmount and remount
              the whole routed page every time the panel is toggled, throwing
              away its scroll position and local state. */}
          <ResizablePanelGroup
            orientation="horizontal"
            groupRef={split.groupRef}
            onLayoutChanged={onLayoutChanged}
            className="min-h-0 min-w-0 flex-1"
          >
            <ResizablePanel
              id="page"
              minSize="40%"
              className="flex min-w-0 flex-col overflow-hidden"
            >
              {children}
            </ResizablePanel>
            {/* Beside the page rather than over it: the point of asking from a
                table is to keep looking at the table. Mounted in the shell so a
                run survives navigation and so the header's button works
                everywhere. */}
            {everOpened ? (
              <>
                <ResizableHandle
                  withHandle
                  // No seam and no grip while the column is shut, or the page
                  // ends in a divider with nothing on the far side of it.
                  className={cn(!assistantOpen && "hidden")}
                />
                <ResizablePanel
                  id="assistant"
                  panelRef={panelRef}
                  collapsible
                  collapsedSize="0%"
                  defaultSize="32%"
                  minSize="22%"
                  maxSize="60%"
                  className="flex min-w-0 flex-col"
                >
                  <AssistantSidePanel />
                </ResizablePanel>
              </>
            ) : null}
          </ResizablePanelGroup>
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
