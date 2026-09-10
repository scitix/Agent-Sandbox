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
import { createPortal } from "react-dom"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useAtom, useAtomValue, useSetAtom } from "jotai"
import { MCP } from "@lobehub/icons"
import {
  AlignLeftIcon,
  CheckIcon,
  CopyIcon,
  FolderClosed,
  FolderOpen,
  KeyIcon,
  SquarePenIcon,
  X,
} from "lucide-react"
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Spinner } from "@/components/ui/spinner"
import { usePersistedLayout } from "@/hooks/use-persisted-layout"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { authAtom, clustersAtom } from "@/lib/atoms"
import { mcpGuide } from "@/components/assistant-ui/mcp-guide"
import { MarkdownContent } from "@/components/assistant-ui/markdown-text"
import { usePathname } from "next/navigation"
import { clusterFromPath } from "@/lib/assistant/current-page"
import { useElementWidth } from "@/hooks/use-element-width"
import {
  atomAssistantMenuOpen,
  atomAssistantOpen,
  atomAssistantCurrentSessionId,
  atomAssistantPage,
  atomAssistantPendingAutoSend,
  atomWorkspaceOpen,
} from "@/lib/assistant/store"
import { Thread } from "@/components/assistant-ui/thread"
import { AssistantMenu } from "@/components/assistant-ui/assistant-menu"
import { AssistantSettings } from "@/components/assistant-ui/assistant-settings"
import { WorkspacePanel } from "@/components/assistant-ui/workspace-panel"
import {
  AssistantLanding,
  useComposerPrefill,
  type LandingActionSpec,
} from "@/components/assistant-ui/assistant-landing"
import {
  sessionLabel,
  useCurrentSession,
  useHasSessionHistory,
} from "@/components/assistant-ui/session-history"
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

/** How long the copy button holds its "copied" face. */
const COPIED_FEEDBACK_MS = 1500

/**
 * "Use AgentBox from your own tools" — a floating entry into the walkthrough.
 *
 * Portalled to `document.body`, and it has to be: the dashboard's main column
 * declares `@container`, `container-type` implies layout containment, and a
 * contained ancestor becomes the containing block for `position: fixed`
 * descendants. Rendered in place, the button would pin itself to the corner of
 * the conversation column and travel with it.
 *
 * Page mode only. In the panel the conversation is already a third of the width
 * and a bubble over it would cover the thing it is offering to explain.
 */
function McpEntry({ cluster }: { cluster: string }) {
  const { t } = useTranslation()
  const locale = useLocale()
  const { clusters } = useAtomValue(clustersAtom)
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  const entry = clusters.find(c => c.id === cluster)
  const guide = mcpGuide(
    { e2bURL: entry?.gateway?.e2bURL, dataURL: entry?.gateway?.dataURL },
    locale
  )

  const copyAll = () => {
    void navigator.clipboard.writeText(guide).then(() => {
      setCopied(true)
      window.setTimeout(() => setCopied(false), COPIED_FEEDBACK_MS)
    })
  }

  // There is no `document` while this renders on the server, and `createPortal`
  // has no server form to fall back on. Nothing is lost by sitting the pass out:
  // the portal's content never occupies this position in the tree, so the markup
  // React hydrates against is the same either way.
  if (typeof document === "undefined") return null

  return createPortal(
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={t("assistant.mcp.title")}
            // Below the z-index dialogs and sheets take (50), so anything modal
            // covers it rather than being punched through by a help bubble.
            className="bg-primary hover:bg-primary/90 fixed bottom-6 right-6 z-30 flex items-center gap-2 rounded-full py-1.5 pe-3.5 ps-1.5 text-xs font-medium text-white shadow-lg transition-colors"
          >
            {/* The mark takes the button's own colour out of a white disc, so
                the entry reads as the protocol's badge rather than as a
                monochrome glyph that disappears into the fill — and, in dark
                mode, went black. */}
            <span className="text-primary flex size-6 items-center justify-center rounded-full bg-white">
              <MCP size={14} />
            </span>
            {t("assistant.mcp.label")}
          </button>
        }
      />
      <PopoverContent
        side="top"
        align="end"
        className="flex max-h-[70vh] w-[30rem] flex-col gap-0 p-0"
      >
        <div className="flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3">
          <PopoverTitle className="text-[13px]">
            {t("assistant.mcp.title")}
          </PopoverTitle>
          <Button
            variant="ghost"
            size="icon"
            className="-me-2 size-7 shrink-0 rounded-full"
            aria-label={t("common.close")}
            onClick={() => setOpen(false)}
          >
            <X className="size-4" />
          </Button>
        </div>

        {/* Nothing floats over the document: a button pinned inside a scroller
            scrolls with it, and pinning it outside means wrapping the scroller
            in a second box that breaks the scroll it was supposed to leave
            alone. The copy action lives in the footer instead, next to the
            other thing you can do from here. */}
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          <MarkdownContent content={guide} />
        </div>

        <div className="flex shrink-0 items-center gap-2 border-t p-3">
          <Button variant="secondary" className="gap-1.5" onClick={copyAll}>
            {copied ? (
              <CheckIcon className="size-4" />
            ) : (
              <CopyIcon className="size-4" />
            )}
            {copied ? t("common.copied") : t("assistant.mcp.copyAll")}
          </Button>
          <Button
            className="flex-1 gap-1.5"
            render={
              <Link
                href={clusterPath(cluster, "api-keys", locale)}
                onClick={() => setOpen(false)}
              >
                <KeyIcon className="size-4" />
                {t("assistant.mcp.apiKey")}
              </Link>
            }
          />
        </div>
      </PopoverContent>
    </Popover>,
    document.body
  )
}

/**
 * The conversation, plus the landing it shows while empty.
 *
 * The landing is a PAGE-mode thing. The panel is a third of the width, the
 * status board needs three tiles across before it is a board rather than a
 * column, and the suggestion pills would wrap to one per line — so the panel
 * keeps the centred greeting and nothing else.
 *
 * A suggestion pre-fills. A status card SENDS, and navigates on the way: the
 * number it quotes lives on a list page, and the answer is worth reading beside
 * the rows it is about. That asymmetry is deliberate — a pill is words you are
 * expected to edit, a card is a question that is already complete.
 */
function ThreadWithLanding({
  mode,
  landingActions,
}: {
  mode: "page" | "panel"
  landingActions?: LandingActionSpec[]
}) {
  const router = useRouter()
  const locale = useLocale()
  const pathname = usePathname()
  const auth = useAtomValue(authAtom)
  const cluster = clusterFromPath(pathname) ?? auth?.clusterID
  const prefill = useComposerPrefill()
  const setAutoSend = useSetAtom(atomAssistantPendingAutoSend)
  const setAssistantOpen = useSetAtom(atomAssistantOpen)

  if (mode !== "page" || !cluster) return <Thread />

  return (
    <Thread
      landing={
        <AssistantLanding
          actions={landingActions}
          onPick={prefill}
          onAsk={ask => {
            // Queued rather than sent here: this component is about to be
            // unmounted by the navigation below, and the panel that picks the
            // conversation up on the destination page is the one that can send.
            setAutoSend(ask.prompt)
            setAssistantOpen(true)
            router.push(clusterPath(cluster, ask.page, locale))
          }}
        />
      }
    />
  )
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
  // Above every early return: the split is remembered per mode, so the panel
  // page and the side column do not fight over one stored layout.
  const surfaceSplit = usePersistedLayout(
    mode === "page"
      ? "agentbox.assistant.surface.page.v1"
      : "agentbox.assistant.surface.panel.v1"
  )
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
  const { newSession } = useSurfaceSessionActions()

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
        // reading. Against the canvas the fill alone is what separates it —
        // which is why the border below is a hairline on the edge of a real
        // surface change rather than a fence drawn to invent one.
        "bg-card relative flex h-full min-h-0 min-w-0 flex-1 flex-col",
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
        onNewSession={newSession}
        onClose={() => setAssistantOpen(false)}
      />
      {view === "config" ? (
        <AssistantSettings onClose={() => setPage(null)} />
      ) : (
        <ThreadWithLanding mode={mode} landingActions={landingActions} />
      )}
    </div>
  )

  return (
    <div
      ref={rootRef}
      // The canvas the panels float on. It was a translucent muted wash back
      // when canvas and panel were the same white and something had to stand in
      // for the missing layer; the token now carries that difference itself.
      className="bg-background flex min-h-0 min-w-0 flex-1 flex-col"
    >
      {showMenu || showWorkspace ? (
        <ResizablePanelGroup
          // Keyed by which columns are out: each combination is its own split,
          // and restoring a two-column layout into a three-column group puts
          // the widths on the wrong panels.
          key={`${showMenu ? "m" : ""}${showWorkspace ? "w" : ""}`}
          groupRef={surfaceSplit.groupRef}
          onLayoutChanged={surfaceSplit.onLayoutChanged}
          className="min-h-0 flex-1"
        >
          {showMenu ? (
            <>
              <ResizablePanel
                id="menu"
                defaultSize="20%"
                minSize="14%"
                maxSize="34%"
                className="flex min-w-0 flex-col"
              >
                <MenuColumn />
              </ResizablePanel>
              {/* Invisible at rest on purpose: the gap between the menu and
                  the card IS the seam, and a painted divider on top of it draws
                  a second one. But a seam nobody can find is not a seam they
                  can drag — so the hit area is widened and the grip fades in
                  under the cursor, which is the only moment it says anything. */}
              <ResizableHandle
                withHandle
                className="group bg-transparent after:w-3 [&>div]:opacity-0 [&>div]:transition-opacity hover:[&>div]:opacity-100 [&[data-dragging]>div]:opacity-100"
              />
            </>
          ) : null}
          <ResizablePanel
            id="conversation"
            minSize="30%"
            className={cn(
              "flex min-w-0 flex-col",
              // Inset only when something is beside it — the padding is what
              // lets the card read as floating, and a card alone in the frame
              // has nothing to float above.
              (showMenu || showWorkspace) && "py-2 pr-2",
              showMenu && "pl-0"
            )}
          >
            {conversation}
          </ResizablePanel>
          {showWorkspace ? (
            <>
              <ResizableHandle
                withHandle
                className="group bg-transparent after:w-3 [&>div]:opacity-0 [&>div]:transition-opacity hover:[&>div]:opacity-100 [&[data-dragging]>div]:opacity-100"
              />
              <ResizablePanel
                id="workspace"
                defaultSize="26%"
                minSize="18%"
                maxSize="45%"
                className="flex min-w-0 flex-col"
              >
                <WorkspacePanel />
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
      {mode === "page" && cluster ? <McpEntry cluster={cluster} /> : null}
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

/**
 * The conversation's own name, centred in the page header row.
 *
 * Absent on an unnamed conversation rather than filled with a placeholder: a
 * new conversation has no title until the gateway names it from the first
 * exchange, and "Untitled" sitting there for those few seconds is a label that
 * says nothing and then changes.
 */
const ConversationTitle: FC = () => {
  const { session } = useCurrentSession()
  const title = session ? sessionLabel(session, "") : ""
  if (!title) return null
  return (
    <span className="text-muted-foreground min-w-0 truncate px-1 text-[13px]">
      {title}
    </span>
  )
}

const SurfaceHeader: FC<{
  mode: "page" | "panel"
  menuOut: boolean
  workspaceOut: boolean
  onNewSession: () => void
  onToggleMenu: () => void
  onToggleWorkspace: () => void
  onClose: () => void
}> = ({
  mode,
  menuOut,
  workspaceOut,
  onNewSession,
  onToggleMenu,
  onToggleWorkspace,
  onClose,
}) => {
  const { t } = useTranslation()
  // The agent's scratch directory only exists once a tool has run in it.
  const hasSession = !!useAtomValue(atomAssistantCurrentSessionId)

  // The panel says what it is and how to shut it, and nothing else. Its actions
  // sit on their own row below, because in a column this narrow a title flanked
  // by four icon buttons has no room left to be a title — and because those
  // actions do not act HERE: each one goes to the page, where the column it
  // opens actually fits.
  if (mode === "panel") {
    return (
      <>
        {/* h-13, matching the page header and the app sidebar's own
            header. The three sit side by side across the top of the
            window, so the rule under this one has to land on the same
            line as the other two or the window looks stepped. */}
        <div className="flex h-13 shrink-0 items-center gap-1 border-b px-3">
          <span className="truncate text-sm font-medium">
            {t("assistant.title")}
          </span>
          <Button
            variant="ghost"
            size="icon"
            className="ml-auto size-7 rounded-full"
            onClick={onClose}
            aria-label={t("assistant.close")}
          >
            <X className="size-4" />
          </Button>
        </div>
        <div className="flex shrink-0 items-center gap-1 px-2 pt-2">
          <Button
            variant="ghost"
            size="icon"
            className="size-7 rounded-full"
            onClick={onToggleMenu}
            aria-label={t("assistant.sessions")}
          >
            <AlignLeftIcon className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-7 rounded-full"
            onClick={onNewSession}
            aria-label={t("assistant.newSession")}
          >
            <SquarePenIcon className="size-4" />
          </Button>
        </div>
      </>
    )
  }

  return (
    <div className="flex h-11 shrink-0 items-center gap-1 px-2">
      {/* ONE toggle on screen at a time, and it lives on whichever side the
          menu currently is. Open, the menu carries it in its own header; closed,
          it reappears here. Showing both put two identical glyphs a few pixels
          apart, which reads as two controls that must do different things. */}
      {menuOut ? null : (
        <Button
          variant="ghost"
          size="icon"
          className="size-7 rounded-full"
          onClick={onToggleMenu}
          aria-label={t("assistant.menu.expand")}
        >
          <AlignLeftIcon className="size-4" />
        </Button>
      )}
      {/* Which CONVERSATION, not which screen. The app header above already
          draws the breadcrumb, so repeating "Assistant" here labelled the same
          thing twice and left the row otherwise empty; the conversation's own
          name is the one thing this strip knows that the breadcrumb does not. */}
      <ConversationTitle />
      {/* A workspace only exists once the agent has run a tool, and until then
          the button's whole effect is to open a panel explaining that there is
          nothing to see. Hiding it means the folder appearing IS the signal
          that there is now something in it. */}
      {hasSession ? (
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto size-7 rounded-full"
          onClick={onToggleWorkspace}
          aria-label={t("workspace.title")}
        >
          {workspaceOut ? (
            <FolderOpen className="size-4" />
          ) : (
            <FolderClosed className="size-4" />
          )}
        </Button>
      ) : null}
    </div>
  )
}
