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

import { useCallback, useEffect, useRef, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { FileText, Folder, Loader2, Paperclip, RefreshCw, Send, Square, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Empty } from "@/components/ui/empty"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"
import { getToken } from "@/lib/api/client"
import {
  type AgentThread,
  createThread,
  deleteThread,
  exportThread,
  fetchCapabilities,
  interruptThread,
  listThreads,
  listWorkspace,
  runTurn,
  stageAttachment,
} from "@/lib/agent-preview"

/** One rendered bubble. Tool calls are their own kind so they can be styled as
 *  activity rather than as something the agent said. */
interface Bubble {
  id: string
  role: "user" | "assistant" | "tool"
  text: string
  toolName?: string
}

let bubbleSeq = 0
const nextBubbleId = () => `b${++bubbleSeq}`

/**
 * The in-console conversation with one deployed agent.
 *
 * Everything goes through the console's streaming proxy, so this component never
 * learns the Brain's address and never names a user — the proxy pins the identity
 * and the gateway reports back which one it used.
 */
export function AgentPreviewPanel({ agent, ready }: { agent: string; ready: boolean }) {
  const { t } = useTranslation()
  // The same accessor the OpenAPI client's middleware uses, so the console speaks
  // to an agent with exactly the credential it uses everywhere else.
  const token = getToken()
  const qc = useQueryClient()

  const [threadId, setThreadId] = useState<string | null>(null)
  const [bubbles, setBubbles] = useState<Bubble[]>([])
  const [draft, setDraft] = useState("")
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const threadsQuery = useQuery({
    queryKey: ["agent-threads", agent],
    queryFn: () => listThreads(agent, token),
    enabled: ready && !!token,
  })

  // The workspace directory the file API demands. Fetched rather than assembled:
  // the identity it derives from is decided by the proxy, so this side cannot
  // reconstruct it.
  const capsQuery = useQuery({
    queryKey: ["agent-capabilities", agent],
    queryFn: () => fetchCapabilities(agent, token),
    enabled: ready && !!token,
  })

  const workspaceQuery = useQuery({
    queryKey: ["agent-workspace", agent, threadId],
    queryFn: () =>
      listWorkspace(agent, token, {
        threadId: threadId as string,
        dir: capsQuery.data?.workspaceDir as string,
      }),
    enabled: ready && !!token && !!threadId && !!capsQuery.data?.workspaceDir,
  })

  // Follow the tail while a turn streams. Only when already near the bottom, so
  // scrolling up to read an earlier answer is not yanked back on every delta.
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 200
    if (nearBottom) el.scrollTop = el.scrollHeight
  }, [bubbles])

  const openThread = useCallback(
    async (id: string) => {
      setThreadId(id)
      setError(null)
      setBubbles([])
      try {
        const entries = await exportThread(agent, token, id)
        setBubbles(
          entries.flatMap((entry) =>
            entry.parts.flatMap((part): Bubble[] => {
              if (part.type === "text" && typeof part.text === "string" && part.text) {
                return [
                  {
                    id: nextBubbleId(),
                    role: entry.role === "user" ? "user" : "assistant",
                    text: part.text,
                  },
                ]
              }
              if (part.type === "tool-call") {
                return [
                  {
                    id: nextBubbleId(),
                    role: "tool",
                    text: "",
                    toolName: String((part as { name?: unknown }).name ?? "tool"),
                  },
                ]
              }
              return []
            }),
          ),
        )
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
      }
    },
    [agent, token],
  )

  const newThread = useMutation({
    mutationFn: () => createThread(agent, token),
    onSuccess: async (id) => {
      await qc.invalidateQueries({ queryKey: ["agent-threads", agent] })
      void openThread(id)
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const removeThread = useMutation({
    mutationFn: (id: string) => deleteThread(agent, token, id),
    onSuccess: async (_r, id) => {
      if (id === threadId) {
        setThreadId(null)
        setBubbles([])
      }
      await qc.invalidateQueries({ queryKey: ["agent-threads", agent] })
    },
  })

  async function send() {
    const text = draft.trim()
    if (!text || streaming) return
    // A first message with no conversation open creates one, so the user never has
    // to press "new" before they can type.
    let id = threadId
    if (!id) {
      try {
        id = await createThread(agent, token)
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
        return
      }
      setThreadId(id)
      void qc.invalidateQueries({ queryKey: ["agent-threads", agent] })
    }

    setDraft("")
    setError(null)
    setBubbles((b) => [...b, { id: nextBubbleId(), role: "user", text }])
    setStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller
    const assistantId = nextBubbleId()
    let opened = false

    try {
      for await (const event of runTurn(agent, token, {
        threadId: id,
        text,
        signal: controller.signal,
      })) {
        if (event.kind === "text" || event.kind === "reasoning") {
          // Reasoning is folded into the same bubble rather than dropped: an agent
          // that thinks for a minute before writing anything otherwise looks stuck.
          const delta = event.delta
          setBubbles((b) => {
            if (!opened) {
              opened = true
              return [...b, { id: assistantId, role: "assistant", text: delta }]
            }
            return b.map((x) => (x.id === assistantId ? { ...x, text: x.text + delta } : x))
          })
        } else if (event.kind === "tool") {
          setBubbles((b) => [
            ...b,
            { id: nextBubbleId(), role: "tool", text: "", toolName: event.name },
          ])
          opened = false
        } else if (event.kind === "error") {
          setError(event.message)
        }
      }
    } catch (e) {
      // An abort is the stop button working, not a failure to report.
      if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
    } finally {
      setStreaming(false)
      abortRef.current = null
      // The agent may have written files, and the thread's title arrives after the
      // turn from a separate model call.
      void qc.invalidateQueries({ queryKey: ["agent-workspace", agent, id] })
      void qc.invalidateQueries({ queryKey: ["agent-threads", agent] })
    }
  }

  async function stop() {
    abortRef.current?.abort()
    if (threadId) await interruptThread(agent, token, threadId)
  }

  async function attach(file: File) {
    if (!threadId || !capsQuery.data?.workspaceDir) return
    try {
      const path = await stageAttachment(agent, token, {
        threadId,
        dir: capsQuery.data.workspaceDir,
        file,
      })
      // The sandbox-side path is appended to the draft, because the agent reads the
      // file from there — an upload the prompt never mentions is invisible to it.
      setDraft((d) => `${d}${d ? "\n" : ""}${t("managedAgents.preview.attachedAs", { path })}`)
    } catch (e) {
      setError(`${t("managedAgents.preview.attachFailed")}: ${e instanceof Error ? e.message : e}`)
    }
  }

  if (!ready) {
    return (
      <Empty>
        <div className="text-sm font-medium">{t("managedAgents.preview.notReady")}</div>
        <div className="text-muted-foreground text-sm">
          {t("managedAgents.preview.notReadyHint")}
        </div>
      </Empty>
    )
  }

  return (
    <div className="grid h-[calc(100vh-19rem)] min-h-[28rem] grid-cols-1 gap-4 lg:grid-cols-[15rem_1fr_16rem]">
      {/* conversations */}
      <Card className="flex flex-col overflow-hidden p-0">
        <div className="flex items-center justify-between border-b px-3 py-2">
          <span className="text-xs font-medium tracking-wide uppercase">
            {t("managedAgents.preview.threads")}
          </span>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => newThread.mutate()}
            disabled={newThread.isPending}
          >
            {t("managedAgents.preview.newThread")}
          </Button>
        </div>
        <ScrollArea className="flex-1">
          {threadsQuery.isLoading ? (
            <div className="space-y-2 p-3">
              <Skeleton className="h-6 w-full" />
              <Skeleton className="h-6 w-full" />
            </div>
          ) : !threadsQuery.data?.length ? (
            <p className="text-muted-foreground p-3 text-sm">
              {t("managedAgents.preview.noThreads")}
            </p>
          ) : (
            <ul className="p-1">
              {threadsQuery.data.map((thread: AgentThread) => (
                <li key={thread.id} className="group flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => void openThread(thread.id)}
                    className={cn(
                      "flex-1 truncate rounded px-2 py-1.5 text-left text-sm",
                      thread.id === threadId ? "bg-accent font-medium" : "hover:bg-accent/50",
                    )}
                  >
                    {thread.title || thread.id}
                  </button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7 opacity-0 group-hover:opacity-100"
                    aria-label={t("managedAgents.preview.deleteThread")}
                    onClick={() => removeThread.mutate(thread.id)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>
      </Card>

      {/* transcript + composer */}
      <Card className="flex flex-col overflow-hidden p-0">
        <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto p-4">
          {!threadId && !bubbles.length ? (
            <p className="text-muted-foreground text-sm">
              {t("managedAgents.preview.selectThread")}
            </p>
          ) : null}
          {bubbles.map((bubble) =>
            bubble.role === "tool" ? (
              <div
                key={bubble.id}
                className="text-muted-foreground flex items-center gap-2 font-mono text-xs"
              >
                <span className="bg-muted rounded px-1.5 py-0.5">
                  {t("managedAgents.preview.toolCall")}
                </span>
                {bubble.toolName}
              </div>
            ) : (
              <div
                key={bubble.id}
                className={cn(
                  "max-w-[85%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap",
                  bubble.role === "user"
                    ? "bg-primary text-primary-foreground ml-auto"
                    : "bg-muted",
                )}
              >
                {bubble.text}
              </div>
            ),
          )}
          {streaming ? (
            <div className="text-muted-foreground flex items-center gap-2 text-xs">
              <Loader2 className="size-3.5 animate-spin" />
              {t("managedAgents.preview.thinking")}
            </div>
          ) : null}
          {error ? <p className="text-destructive text-xs whitespace-pre-wrap">{error}</p> : null}
        </div>

        <div className="flex items-end gap-2 border-t p-3">
          <label className="shrink-0">
            <input
              type="file"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0]
                e.target.value = ""
                if (file) void attach(file)
              }}
            />
            <Button
              size="icon"
              variant="ghost"
              render={<span />}
              aria-label={t("managedAgents.preview.attach")}
            >
              <Paperclip className="size-4" />
            </Button>
          </label>
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              // Enter sends; Shift+Enter is a newline. A multi-line prompt is common
              // enough that swallowing Shift+Enter would be the wrong default.
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
            rows={2}
            placeholder={t("managedAgents.preview.placeholder")}
            className="max-h-40 min-h-10 flex-1 resize-none"
          />
          {streaming ? (
            <Button size="icon" variant="secondary" onClick={() => void stop()}>
              <Square className="size-4" />
            </Button>
          ) : (
            <Button size="icon" onClick={() => void send()} disabled={!draft.trim()}>
              <Send className="size-4" />
            </Button>
          )}
        </div>
      </Card>

      {/* the sandbox's workspace */}
      <Card className="flex flex-col overflow-hidden p-0">
        <div className="flex items-center justify-between border-b px-3 py-2">
          <span className="text-xs font-medium tracking-wide uppercase">
            {t("managedAgents.preview.workspace")}
          </span>
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            aria-label={t("managedAgents.preview.refreshWorkspace")}
            onClick={() => void workspaceQuery.refetch()}
          >
            <RefreshCw className="size-3.5" />
          </Button>
        </div>
        <ScrollArea className="flex-1">
          {/* `inactive` and `expired` are the sandbox's own answers and are not
              errors: the first means no sandbox has been created for this
              conversation yet, the second that it was reclaimed. Showing an empty
              list for either would say the files are gone when there were none. */}
          {!threadId ? null : workspaceQuery.data?.status === "ok" &&
            workspaceQuery.data.entries?.length ? (
            <ul className="p-2 text-sm">
              {workspaceQuery.data.entries.map((entry) => (
                <li key={entry.path} className="flex items-center gap-2 px-1 py-1">
                  {entry.isDir ? (
                    <Folder className="text-muted-foreground size-3.5 shrink-0" />
                  ) : (
                    <FileText className="text-muted-foreground size-3.5 shrink-0" />
                  )}
                  <span className="truncate font-mono text-xs">{entry.name}</span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground p-3 text-xs">
              {t("managedAgents.preview.emptyWorkspace")}
            </p>
          )}
        </ScrollArea>
      </Card>
    </div>
  )
}
