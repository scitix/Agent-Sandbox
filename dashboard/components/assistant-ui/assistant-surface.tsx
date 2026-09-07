"use client"

/**
 * ONE assistant. Two places it is mounted.
 *
 * The dedicated page and the right-hand panel are the same component, and the
 * difference is stated once as a `mode` — because when they were two shells
 * around the same thread in the implementation this is modelled on, they
 * drifted: different headers, different actions, a landing on one and not the
 * other.
 *
 *   * `page` owns the conversation menu (a resizable column) and the landing,
 *     because it has the room.
 *   * `panel` has neither. Its menu button NAVIGATES to the page — the panel is
 *     the page with everything closed, so "open the menu" means "go where it
 *     fits".
 *
 * "Has the room" is measured rather than assumed: below MENU_BREAKPOINT the
 * menu stops being a column and becomes a sheet, the same trade the app's own
 * sidebar makes on a phone. Measured on THIS element, not the window, because
 * the panel spends the width before the assistant sees any of it.
 */

import { useEffect, useState, type FC, type ReactNode } from "react"
import { useRouter } from "next/navigation"
import { useAtom, useSetAtom } from "jotai"
import { PanelLeftClose, PanelLeftOpen, Plus, X } from "lucide-react"
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"
import { useLocale } from "@/hooks/use-locale"
import { useElementWidth } from "@/hooks/use-element-width"
import {
  atomAssistantMenuOpen,
  atomAssistantOpen,
} from "@/lib/assistant/store"
import { Thread } from "@/components/assistant-ui/thread"
import {
  AssistantLanding,
  type LandingActionSpec,
} from "@/components/assistant-ui/assistant-landing"
import {
  SessionHistoryList,
  useHasSessionHistory,
} from "@/components/assistant-ui/session-history"
import {
  useAssistantLoadState,
  useAssistantSessionStore,
} from "@/components/assistant-ui/backend-port"

/**
 * Below this many pixels of surface, the menu is a sheet.
 *
 * The menu takes a fifth of the width and the conversation the rest, so a
 * ~900px surface already leaves the menu too narrow to read a title in and the
 * conversation too narrow to read a table in. Past that point one of them has
 * to go, and an overlay is how you keep both.
 */
const MENU_BREAKPOINT = 900

export interface AssistantSurfaceProps {
  mode: "page" | "panel"
  landingActions?: LandingActionSpec[]
}

/**
 * Whether the menu is out, and how to change that.
 *
 * The toggle is tri-state; this is where the untouched case gets its answer,
 * and the answer is the history: a user with no conversations has nothing to
 * put in a menu, so the page opens as a single column and the menu appears on
 * its own once there IS something to list. A user who collapsed it keeps it
 * collapsed for the rest of the visit.
 */
function useMenuOpen(): [boolean, (open: boolean) => void] {
  const [choice, setChoice] = useAtom(atomAssistantMenuOpen)
  const hasHistory = useHasSessionHistory()
  return [choice ?? hasHistory, setChoice]
}

export const AssistantSurface: FC<AssistantSurfaceProps> = ({
  mode,
  landingActions,
}) => {
  const { t } = useTranslation()
  const locale = useLocale()
  const router = useRouter()
  const loadState = useAssistantLoadState()
  const sessions = useAssistantSessionStore()
  const [menuOpen, setMenuOpen] = useMenuOpen()
  const setAssistantOpen = useSetAtom(atomAssistantOpen)
  const [rootRef, width] = useElementWidth()
  const [sheetOpen, setSheetOpen] = useState(false)

  const compact = width != null && width < MENU_BREAKPOINT
  const showMenuColumn = mode === "page" && menuOpen && !compact && !!sessions

  // From the panel, the menu is a place rather than a toggle: it does not fit
  // in a third of the width, so the button goes where it does.
  const openMenu = () => {
    if (mode === "panel") {
      setMenuOpen(true)
      setAssistantOpen(false)
      router.push(`/${locale}/assistant`)
      return
    }
    if (compact) setSheetOpen(true)
    else setMenuOpen(true)
  }

  if (loadState === "loading") {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
        <Spinner className="size-4" />
        {t("assistant.initializingSession")}
      </div>
    )
  }

  const header = (
    <div className="flex h-11 shrink-0 items-center gap-1 border-b px-2">
      {!showMenuColumn ? (
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={openMenu}
          aria-label={t("assistant.menu.expand")}
        >
          <PanelLeftOpen className="size-4" />
        </Button>
      ) : (
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={() => setMenuOpen(false)}
          aria-label={t("assistant.menu.collapse")}
        >
          <PanelLeftClose className="size-4" />
        </Button>
      )}
      <span className="truncate text-sm font-medium">
        {t("assistant.title")}
      </span>
      <div className="ml-auto flex items-center gap-1">
        {sessions ? (
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={() => void sessions.create()}
            aria-label={t("assistant.newSession")}
          >
            <Plus className="size-4" />
          </Button>
        ) : null}
        {mode === "panel" ? (
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={() => setAssistantOpen(false)}
            aria-label={t("assistant.close")}
          >
            <X className="size-4" />
          </Button>
        ) : null}
      </div>
    </div>
  )

  const conversation = (
    <div
      className={cn(
        // The conversation is the panel: it keeps its own fill whether or not
        // the menu is out, so collapsing the menu does not repaint what you
        // are reading.
        "bg-card relative flex min-h-0 min-w-0 flex-1 flex-col",
        showMenuColumn && "overflow-hidden rounded-xl border"
      )}
    >
      {header}
      <Thread
        landing={
          mode === "page" ? (
            <AssistantLanding
              {...(landingActions ? { landingActions } : {})}
            />
          ) : undefined
        }
      />
    </div>
  )

  return (
    <div ref={rootRef} className="flex min-h-0 min-w-0 flex-1 flex-col">
      {showMenuColumn ? (
        <ResizablePanelGroup className="min-h-0 flex-1">
          <ResizablePanel defaultSize="20%" minSize="14%" maxSize="34%">
            <MenuColumn />
          </ResizablePanel>
          <ResizableHandle />
          <ResizablePanel>{conversation}</ResizablePanel>
        </ResizablePanelGroup>
      ) : (
        conversation
      )}
      {mode === "page" && sessions ? (
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent side="left" className="w-[85vw] max-w-sm p-0">
            <SheetTitle className="sr-only">
              {t("assistant.sessions")}
            </SheetTitle>
            <MenuColumn onOpenChat={() => setSheetOpen(false)} />
          </SheetContent>
        </Sheet>
      ) : null}
    </div>
  )
}

const MenuColumn: FC<{ onOpenChat?: () => void }> = ({ onOpenChat }) => (
  <div className="flex h-full min-h-0 flex-col">
    <SessionHistoryList {...(onOpenChat ? { onOpenChat } : {})} />
  </div>
)

/** Closes the panel when the route changes, so it never follows a navigation. */
export const AssistantPanelRouteGuard: FC<{ children: ReactNode }> = ({
  children,
}) => {
  const setOpen = useSetAtom(atomAssistantOpen)
  useEffect(() => () => setOpen(false), [setOpen])
  return <>{children}</>
}
