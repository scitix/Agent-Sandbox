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
import { useAtom, useAtomValue } from "jotai"
import { Sparkles } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { authAtom, lastSeenVersionAtom, changelogDialogOpenAtom } from "@/lib/atoms"
import { hasUnseenVersion } from "@/lib/version"
import { latestVersion } from "@/lib/changelog"
import { ChangelogDialogContent } from "@/components/changelog/changelog-content"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"

// Derived from the latest entry in CHANGELOG — no build-time env var needed
const APP_VERSION = latestVersion()

// ── Inner (mounted only when open) ───────────────────────────────────────────

function ChangelogDialogInner({
  onClose,
  onMarkRead,
}: {
  onClose: () => void
  onMarkRead: () => void
}) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const router = useRouter()
  const [dontShowAgain, setDontShowAgain] = useState(false)

  const handleGotIt = useCallback(() => {
    onMarkRead()
    onClose()
  }, [onMarkRead, onClose])

  const handleFullHistory = useCallback(() => {
    onMarkRead()
    onClose()
    router.push(`/${clusterID ? `clusters/${clusterID}/` : ""}changelog`)
  }, [onMarkRead, onClose, router, clusterID])

  const handleCloseButton = useCallback(() => {
    if (dontShowAgain) {
      onMarkRead()
    }
    onClose()
  }, [dontShowAgain, onMarkRead, onClose])

  return (
    <>
      <DialogHeader className="shrink-0">
        <DialogTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
          <Sparkles className="text-brand h-4 w-4" />
          {t("changelog.whatsNewInVersion", { version: latestVersion() })}
        </DialogTitle>
        <DialogDescription className="text-muted-foreground text-xs">
          {t("changelog.heresWhatChanged")}
        </DialogDescription>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-hidden">
        <ChangelogDialogContent />
      </div>

      <DialogFooter className="flex shrink-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        {/* Left: "don't show again" checkbox */}
        <label className="flex cursor-pointer items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={dontShowAgain}
            onChange={(e) => setDontShowAgain(e.target.checked)}
            className="accent-brand h-3.5 w-3.5"
          />
          <span className="text-muted-foreground">{t("changelog.dontShowAgain")}</span>
        </label>

        {/* Right: actions */}
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleFullHistory}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {t("changelog.fullHistory")}
          </Button>
          <Button
            size="sm"
            onClick={handleGotIt}
            className="bg-brand hover:bg-brand/90 font-mono text-xs tracking-wider text-white uppercase"
          >
            {t("changelog.gotIt")}
          </Button>
        </div>
      </DialogFooter>

      {/* Override the default close button behavior to respect "don't show again" */}
      <div className="absolute top-4 right-4">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleCloseButton}
          aria-label="Close"
          className="h-6 w-6"
        >
          <span className="text-muted-foreground text-base leading-none">✕</span>
        </Button>
      </div>
    </>
  )
}

// ── Shell (always mounted, manages detection logic) ───────────────────────────

/**
 * Global What's New dialog. Mount once inside AuthGuard in the dashboard layout.
 * Automatically opens after login when a new version is detected.
 */
export function ChangelogDialog() {
  const auth = useAtomValue(authAtom)
  const [lastSeen, setLastSeen] = useAtom(lastSeenVersionAtom)
  const [open, setOpen] = useAtom(changelogDialogOpenAtom)
  const [hydrated, setHydrated] = useState(false)

  // Wait for atomWithStorage to hydrate from localStorage
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  // Auto-open after hydration when there's a new unseen version
  useEffect(() => {
    if (!hydrated) return
    if (!auth) return // Only show when authenticated
    if (hasUnseenVersion(APP_VERSION, lastSeen)) {
      const t = setTimeout(() => setOpen(true), 800)
      return () => clearTimeout(t)
    }
  }, [hydrated, auth, lastSeen, setOpen])

  const markRead = useCallback(() => {
    setLastSeen(APP_VERSION)
  }, [setLastSeen])

  const handleClose = useCallback(() => {
    setOpen(false)
  }, [setOpen])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="border-border bg-card flex flex-col overflow-hidden sm:max-w-xl"
        showCloseButton={false}
      >
        {open && <ChangelogDialogInner onClose={handleClose} onMarkRead={markRead} />}
      </DialogContent>
    </Dialog>
  )
}
