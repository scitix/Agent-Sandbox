"use client"

/**
 * ONE assistant. Two places it is mounted, three columns when there is room.
 *
 * The dedicated page and the right-hand panel are the same component, and the
 * difference is stated once as a `mode` — because when they were two shells
 * around the same thread in the implementation this follows, they drifted:
 * different headers, different actions, a landing on one and not the other.
 *
 *   * `page` owns the conversation menu and the workspace, both resizable
 *     columns, because it has the room.
 *   * `panel` has neither. Its menu and folder buttons NAVIGATE to the page
 *     with that column open — the panel is the page with everything closed, so
 *     "open the workspace" means "go where it fits".
 *
 * "Has the room" is measured, not assumed: below MENU_BREAKPOINT the menu
 * becomes a sheet, the same trade the app's own sidebar makes on a phone. And
 * it is measured on THIS element rather than the window, because the panel
 * spends the width before the assistant ever sees it.
 */

import { useState, type FC } from "react"
import { useRouter } from "next/navigation"
import { useAtom, useAtomValue, useSetAtom } from "jotai"
import {
  FolderClosed,
  FolderOpen,
  PanelLeftClose,
  PanelLeftOpen,
  X,
} from "lucide-react"
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
import { clusterPath } from "@/lib/cluster-path"
import { authAtom } from "@/lib/atoms"
import { usePathname } from "next/navigation"
import { clusterFromPath } from "@/lib/assistant/current-page"
import { useElementWidth } from "@/hooks/use-element-width"
import {
  atomAssistantMenuOpen,
  atomAssistantOpen,
  atomAssistantPage,
  atomWorkspaceOpen,
} from "@/lib/assistant/store"
import { Thread } from "@/components/assistant-ui/thread"
import { AssistantMenu } from "@/components/assistant-ui/assistant-menu"
import { AssistantSettings } from "@/components/assistant-ui/assistant-settings"
import { WorkspacePanel } from "@/components/assistant-ui/workspace-panel"
import {
  AssistantLanding,
  type LandingActionSpec,
} from "@/components/assistant-ui/assistant-landing"
import { useHasSessionHistory } from "@/components/assistant-ui/session-history"
import { useSessionActions } from "@/components/assistant-ui/session-controls"
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

/** Below this, the workspace column would leave the conversation unreadable. */
const WORKSPACE_BREAKPOINT = 1200

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
 * its own once there IS something to list. Once the user has decided, their
 * choice wins for the rest of the visit.
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
  const [workspaceOpen, setWorkspaceOpen] = useAtom(atomWorkspaceOpen)
  const [page, setPage] = useAtom(atomAssistantPage)
  const setAssistantOpen = useSetAtom(atomAssistantOpen)
  // The route names the cluster when we are already inside one; the session's
  // cluster is the fallback for the cluster-less entry point.
  const pathname = usePathname()
  const auth = useAtomValue(authAtom)
  const cluster = clusterFromPath(pathname) ?? auth?.clusterID
  const [rootRef, width] = useElementWidth()
  const [sheetOpen, setSheetOpen] = useState(false)

  const compact = width != null && width < MENU_BREAKPOINT
  const narrow = width != null && width < WORKSPACE_BREAKPOINT
  const showMenu = mode === "page" && menuOpen && !compact && !!sessions
  const showWorkspace = mode === "page" && workspaceOpen && !narrow
  const view = mode === "page" ? page : null

  // From the panel, a column is a PLACE rather than a toggle: neither fits in a
  // third of the width, so the button goes where it does.
  const goToPage = (open: "menu" | "workspace") => {
    if (open === "menu") setMenuOpen(true)
    else setWorkspaceOpen(true)
    setAssistantOpen(false)
    // clusterPath, not an interpolated locale: the default locale is
    // deliberately absent from URLs here, and the i18n middleware rewrites a
    // path that lacks one by PREPENDING it. Hand it `/en/...` and it does not
    // recognise its own prefix, so it prepends again — and again — until the
    // rewrite header overflows and every page answers 431.
    router.push(clusterPath(cluster ?? "default", "assistant", locale))
  }

  if (loadState === "loading") {
    return (
      <div className="text-muted-foreground flex min-h-0 flex-1 items-center justify-center gap-2 text-sm">
        <Spinner className="size-4" />
        {t("assistant.initializingSession")}
      </div>
    )
  }

  const conversation = (
    <div
      className={cn(
        // The conversation is the panel: it keeps its own fill whether or not a
        // column is out, so collapsing one does not repaint what you are
        // reading.
        "bg-card relative flex min-h-0 min-w-0 flex-1 flex-col",
        // The floating look — a card inset with a hairline, rather than a hard
        // seam against the frame. Only when something is beside it; alone it
        // fills the frame and no border is drawn at all.
        (showMenu || showWorkspace) && "overflow-hidden rounded-xl border"
      )}
    >
      <SurfaceHeader
        mode={mode}
        menuOut={showMenu}
        workspaceOut={showWorkspace}
        onToggleMenu={() => {
          if (mode === "panel") return goToPage("menu")
          if (compact) setSheetOpen(true)
          else setMenuOpen(!showMenu)
        }}
        onToggleWorkspace={() => {
          if (mode === "panel") return goToPage("workspace")
          setWorkspaceOpen(!showWorkspace)
        }}
        onClose={() => setAssistantOpen(false)}
      />
      {view === "config" ? (
        <AssistantSettings onClose={() => setPage(null)} />
      ) : (
        <Thread
          landing={
            mode === "page" ? (
              <AssistantLanding
                {...(landingActions ? { landingActions } : {})}
              />
            ) : undefined
          }
        />
      )}
    </div>
  )

  return (
    <div
      ref={rootRef}
      className="bg-muted/30 flex min-h-0 min-w-0 flex-1 flex-col"
    >
      {showMenu || showWorkspace ? (
        <ResizablePanelGroup className="min-h-0 flex-1">
          {showMenu ? (
            <>
              <ResizablePanel defaultSize="20%" minSize="14%" maxSize="34%">
                <MenuColumn />
              </ResizablePanel>
              <ResizableHandle />
            </>
          ) : null}
          <ResizablePanel minSize="30%">{conversation}</ResizablePanel>
          {showWorkspace ? (
            <>
              <ResizableHandle />
              <ResizablePanel defaultSize="26%" minSize="18%" maxSize="45%">
                <div className="flex h-full min-h-0 flex-col p-1 pl-0">
                  <WorkspacePanel />
                </div>
              </ResizablePanel>
            </>
          ) : null}
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

/** The conversation menu, in a column or in the sheet. */
const MenuColumn: FC<{ onOpenChat?: () => void }> = ({ onOpenChat }) => {
  const [, setMenuOpen] = useMenuOpen()
  const setPage = useSetAtom(atomAssistantPage)
  const { newSession, newSessionBusy } = useSurfaceSessionActions()
  const page = useAtomValue(atomAssistantPage)
  return (
    <AssistantMenu
      onCollapse={() => setMenuOpen(false)}
      onNewSession={newSession}
      newSessionBusy={newSessionBusy}
      page={page}
      onOpenPage={setPage}
      onOpenChat={() => {
        setPage(null)
        onOpenChat?.()
      }}
    />
  )
}

/**
 * New-session, wrapped so the menu does not need to know which of the two
 * sources can create one.
 *
 * `useSessionActions` reads thread state and therefore only works inside the
 * runtime; the port's store is what the menu can always reach. Preferring the
 * former keeps the "already on an empty session" guard, which is the only
 * thing stopping a double click from spawning two blank conversations.
 */
function useSurfaceSessionActions() {
  const sessions = useAssistantSessionStore()
  const actions = useSessionActions()
  return {
    newSession: () => {
      if (actions?.newSession) return actions.newSession()
      void sessions?.create()
    },
    newSessionBusy: actions?.newBusy ?? false,
  }
}

const SurfaceHeader: FC<{
  mode: "page" | "panel"
  menuOut: boolean
  workspaceOut: boolean
  onToggleMenu: () => void
  onToggleWorkspace: () => void
  onClose: () => void
}> = ({
  mode,
  menuOut,
  workspaceOut,
  onToggleMenu,
  onToggleWorkspace,
  onClose,
}) => {
  const { t } = useTranslation()
  return (
    <div className="flex h-11 shrink-0 items-center gap-1 border-b px-2">
      <Button
        variant="ghost"
        size="icon"
        className="size-7"
        onClick={onToggleMenu}
        aria-label={menuOut ? t("assistant.menu.collapse") : t("assistant.menu.expand")}
      >
        {menuOut ? (
          <PanelLeftClose className="size-4" />
        ) : (
          <PanelLeftOpen className="size-4" />
        )}
      </Button>
      <span className="truncate text-sm font-medium">
        {t("assistant.title")}
      </span>
      <div className="ml-auto flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={onToggleWorkspace}
          aria-label={t("workspace.title")}
        >
          {workspaceOut ? (
            <FolderOpen className="size-4" />
          ) : (
            <FolderClosed className="size-4" />
          )}
        </Button>
        {mode === "panel" ? (
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={onClose}
            aria-label={t("assistant.close")}
          >
            <X className="size-4" />
          </Button>
        ) : null}
      </div>
    </div>
  )
}
