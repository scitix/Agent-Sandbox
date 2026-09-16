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
 * The floating entry into the CLI guide.
 *
 * On every page of the console, not only the assistant's: the question it
 * answers — "how do I do this myself, from my own machine or my own agent?" —
 * is asked while looking at an Env, a Template, a pool, a sandbox, and the
 * assistant page is the one place where it is already being answered in prose.
 *
 * Portalled to `document.body`, and it has to be: the dashboard's main column
 * declares `@container`, `container-type` implies layout containment, and a
 * contained ancestor becomes the containing block for `position: fixed`
 * descendants. Rendered in place, the button would pin itself to the corner of
 * whatever column it was placed in and travel with it.
 */

import { useState } from "react"
import { createPortal } from "react-dom"
import Link from "next/link"
import { usePathname } from "next/navigation"
import { useAtomValue } from "jotai"
import { MCP } from "@lobehub/icons"
import { CheckIcon, CopyIcon, KeyIcon, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { cliGuide } from "@/components/assistant-ui/cli-guide"
import { authAtom, clustersAtom } from "@/lib/atoms"
import { basePath } from "@/lib/base-path"
import { clusterFromPath } from "@/lib/assistant/current-page"
import { clusterPath } from "@/lib/cluster-path"
import { useTranslation } from "@/lib/i18n"
import { useLocale } from "@/hooks/use-locale"
import { useDocsApiKey } from "@/hooks/use-docs-api-key"
import { useDraggableBall } from "@/hooks/use-draggable-ball"
import { cn } from "@/lib/utils"

/** How long the copy button holds its "copied" face. */
const COPIED_FEEDBACK_MS = 1500

export function CliGuideEntry() {
  const { t } = useTranslation()
  const locale = useLocale()
  const { clusters } = useAtomValue(clustersAtom)
  const auth = useAtomValue(authAtom)
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  // Which cluster the guide's addresses describe. The route names one on most
  // pages; the session's own cluster is the fallback for the handful that sit
  // outside one (the overview, the assistant's cluster-less entry). With
  // neither, the guide renders its placeholders — a document that says where
  // the values go rather than inventing them.
  const pathname = usePathname()
  const cluster = clusterFromPath(pathname) ?? auth?.clusterID

  // Destructured rather than read through `ball.…` inside the markup: the hooks
  // lint reads a property access on an object that holds a ref as a ref access
  // during render, which this is not.
  const {
    ref: ballRef,
    style: ballStyle,
    dragging: ballDragging,
    onPointerDown: onBallPointerDown,
  } = useDraggableBall("agentbox.cli-guide.position.v1")

  const entry = clusters.find(c => c.id === cluster)
  // The console's own address, read off the browser rather than configured:
  // this source is public and one organisation runs several platforms from it,
  // so a URL written into the repository would publish an internal hostname
  // and point every other deployment's readers at the wrong one.
  const consoleBase =
    typeof window === "undefined"
      ? ""
      : `${window.location.origin}${basePath()}`
  // The guide is handed to a coding agent, so it is filled in with an agent-mode
  // key when the reader has one — the credential that matches what the document
  // is for. The document itself never carries one: it arrives with
  // `${AGBX_API_KEY}` standing, and the substitution happens here, in the
  // browser, from the reader's own keys.
  const { fill } = useDocsApiKey("agent")
  const guide = fill(
    cliGuide(
      {
        e2bURL: entry?.gateway?.e2bURL,
        dataURL: entry?.gateway?.dataURL,
        consoleBase,
      },
      locale
    )
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
    <>
      {/* Over everything while a drag is in flight: it stops the pointer being
          taken mid-drag — by text selection, by an iframe, by anything that
          would rather have the events — and carries the grabbing cursor so the
          whole screen agrees about what is happening. */}
      {ballDragging ? <div className="fixed inset-0 z-40 cursor-grabbing" /> : null}
      <Popover
        open={open}
        // A drag ends with a click the browser has already queued. Opening the
        // guide because someone moved the button is the one thing this must not
        // do.
        onOpenChange={next => {
          if (next && ballDragging) return
          setOpen(next)
        }}
      >
        <PopoverTrigger
          render={
            <button
              type="button"
              ref={ballRef as React.RefObject<HTMLButtonElement>}
              style={ballStyle}
              onPointerDown={onBallPointerDown}
              aria-label={t("assistant.mcp.title")}
              // Below the z-index dialogs and sheets take (50), so anything
              // modal covers it rather than being punched through by a help
              // bubble — but not below the drag overlay it is dragged over.
              className={cn(
                "bg-primary hover:bg-primary/90 fixed right-6 bottom-6 z-40 flex touch-none items-center gap-2 rounded-full py-1.5 pe-3.5 ps-1.5 text-xs font-medium shadow-lg transition-colors select-none",
                // Dark mode keeps the brand fill but stops the CONTENTS being
                // white: white text next to a white disc are the two brightest
                // things on a dark screen, and together they made a small
                // control read as a lamp.
                "text-white dark:text-neutral-900",
                ballDragging ? "cursor-grabbing" : "cursor-grab"
              )}
            >
              {/* The MCP mark, keep it: the platform's tools are MCP tools, and
                  the badge is what makes the entry read as "here is the door
                  into this platform" rather than as a terminal icon that could
                  mean anything. The disc darkens with the text; the mark keeps
                  the brand colour, which is the one thing that should still
                  catch the eye. */}
              <span className="text-primary flex size-6 items-center justify-center rounded-full bg-white dark:bg-neutral-900">
                <MCP size={14} />
              </span>
              {t("assistant.mcp.label")}
            </button>
          }
        />
        <PopoverContent
          side="top"
          align="end"
          // Wider than a chat bubble and height-bounded like one: the guide is a
          // document with a table, four code blocks and a numbered spine, and at
          // 30rem it read as a column of fragments. The width stops short of the
          // viewport on a small screen rather than overflowing it.
          className="flex max-h-[80vh] w-[min(46rem,calc(100vw-3rem))] flex-col gap-0 p-0"
        >
          <div className="flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3">
            <PopoverTitle className="text-[13px]">{t("assistant.mcp.title")}</PopoverTitle>
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
            {/* The shared renderer, not the chat one: same Shiki highlighting
                and the same per-block copy button the Env docs have, because
                this is the same kind of thing — a document to copy out of. */}
            <MarkdownRenderer content={guide} />
          </div>

          <div className="flex shrink-0 items-center gap-2 border-t p-3">
            <Button variant="secondary" className="gap-1.5" onClick={copyAll}>
              {copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
              {copied ? t("common.copied") : t("assistant.mcp.copyAll")}
            </Button>
            {cluster ? (
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
            ) : null}
          </div>
        </PopoverContent>
      </Popover>
    </>,
    document.body
  )
}
