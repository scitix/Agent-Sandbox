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

import { useEffect, useRef, useState } from "react"
import { parseAsString, useQueryState } from "nuqs"
import { useParams } from "next/navigation"
import { XIcon, TerminalSquareIcon } from "lucide-react"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useCreateExecToken } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

// ─── nuqs URL param name ───────────────────────────────────────────────────────

export const TERMINAL_SANDBOX_ID_PARAM = "terminalFor"

// ─── Connection status badge ──────────────────────────────────────────────────

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error"

function StatusBadge({ status }: { status: ConnectionStatus }) {
  const { t } = useTranslation()
  const variants: Record<ConnectionStatus, string> = {
    connecting: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
    connected: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30",
    disconnected: "bg-gray-500/15 text-gray-600 dark:text-gray-400 border-gray-500/30",
    error: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  }
  const labelKeys: Record<ConnectionStatus, string> = {
    connecting: t("sandboxes.connecting"),
    connected: t("sandboxes.connected"),
    disconnected: t("sandboxes.disconnected"),
    error: t("sandboxes.error"),
  }
  return (
    <Badge variant="outline" className={`font-mono text-xs uppercase ${variants[status]}`}>
      {labelKeys[status]}
    </Badge>
  )
}

// ─── Inner terminal content ────────────────────────────────────────────────────

interface TerminalContentProps {
  sandboxId: string
  clusterID: string
}

function TerminalContent({ sandboxId, clusterID }: TerminalContentProps) {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<import("@xterm/xterm").Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const [status, setStatus] = useState<ConnectionStatus>("connecting")

  const { mutateAsync: createExecToken } = useCreateExecToken()

  const connect = async () => {
    if (!containerRef.current) return
    setStatus("connecting")

    // Clean up previous terminal and WS
    wsRef.current?.close()
    termRef.current?.dispose()

    let token: string
    try {
      const resp = await createExecToken({
        params: { path: { sandboxId } },
      })
      if (!resp || !resp.token) {
        setStatus("error")
        return
      }
      token = resp.token
    } catch {
      setStatus("error")
      return
    }

    // Dynamically import xterm to avoid SSR issues
    const { Terminal } = await import("@xterm/xterm")
    const { FitAddon } = await import("@xterm/addon-fit")
    const { WebLinksAddon } = await import("@xterm/addon-web-links")

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", "Fira Code", "Cascadia Code", Menlo, monospace',
      theme: {
        background: "#0d1117",
        foreground: "#c9d1d9",
        cursor: "#58a6ff",
        selectionBackground: "#264f78",
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new WebLinksAddon())

    // Clear container and open terminal
    containerRef.current.innerHTML = ""
    term.open(containerRef.current)
    fitAddon.fit()
    term.focus()
    termRef.current = term

    // Build WS URL
    const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""
    const proto = location.protocol === "https:" ? "wss:" : "ws:"
    const wsUrl = `${proto}//${location.host}${basePath}/ws/clusters/${clusterID}/sandboxes/${sandboxId}/terminal?token=${token}`

    const ws = new WebSocket(wsUrl)
    ws.binaryType = "arraybuffer"
    wsRef.current = ws

    ws.onopen = () => {
      setStatus("connected")
      // Send initial resize
      fitAddon.fit()
      ws.send(
        JSON.stringify({
          op: "resize",
          cols: term.cols,
          rows: term.rows,
        }),
      )
    }
    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(event.data))
      } else {
        term.write(event.data as string)
      }
    }
    ws.onclose = () => {
      setStatus("disconnected")
    }
    ws.onerror = () => {
      setStatus("error")
    }

    // Forward terminal input to WS
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ op: "stdin", data }))
      }
    })

    // Resize handler
    const ro = new ResizeObserver(() => {
      fitAddon.fit()
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ op: "resize", cols: term.cols, rows: term.rows }))
      }
    })
    if (containerRef.current) ro.observe(containerRef.current)

    // Clean up observer when terminal is disposed
    const origDispose = term.dispose.bind(term)
    term.dispose = () => {
      ro.disconnect()
      origDispose()
    }
  }

  // Connect on mount
  useEffect(() => {
    void connect()
    return () => {
      wsRef.current?.close()
      termRef.current?.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sandboxId, clusterID])

  return (
    <div className="flex h-full flex-col overflow-hidden rounded-xl">
      {/* Toolbar */}
      <div className="flex shrink-0 items-center justify-between border-b px-4 py-2.5">
        <div className="flex items-center gap-2.5">
          <TerminalSquareIcon className="size-4 text-[#58a6ff]" />
          <span className="text-muted-foreground font-mono text-xs">
            sandbox/{" "}
            <span className="font-semibold text-[#c9d1d9]">
              {sandboxId.length > 16 ? sandboxId.slice(0, 16) + "…" : sandboxId}
            </span>
          </span>
          <StatusBadge status={status} />
        </div>
        <div className="flex items-center gap-2">
          {(status === "disconnected" || status === "error") && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 font-mono text-[11px]"
              onClick={() => void connect()}
            >
              {t("sandboxes.reconnect")}
            </Button>
          )}
          <DialogClose
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-[#8b949e] hover:bg-white/10 hover:text-[#c9d1d9]"
              />
            }
          >
            <XIcon className="size-4" />
            <span className="sr-only">{t("common.close")}</span>
          </DialogClose>
        </div>
      </div>

      {/* Terminal container */}
      <div className="min-h-0 flex-1 bg-[#0d1117] p-2">
        <div ref={containerRef} className="h-full w-full" />
      </div>
    </div>
  )
}

// ─── Shell dialog (URL-driven) ─────────────────────────────────────────────────

export function TerminalDialog() {
  const { t } = useTranslation()
  const params = useParams<{ clusterID: string }>()
  const clusterID = params?.clusterID ?? ""

  const [sandboxId, setSandboxId] = useQueryState(
    TERMINAL_SANDBOX_ID_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )

  const isOpen = !!sandboxId

  const handleOpenChange = (open: boolean) => {
    if (!open) void setSandboxId(null)
  }

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="flex h-170 flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl"
        showCloseButton={false}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>{t("sandboxes.terminalTitle", { sandboxId: sandboxId ?? "" })}</DialogTitle>
        </DialogHeader>
        {/* Mount inner content only when dialog is open so resources are released on close */}
        {isOpen && sandboxId && <TerminalContent sandboxId={sandboxId} clusterID={clusterID} />}
      </DialogContent>
    </Dialog>
  )
}
