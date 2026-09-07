import {
  ActionBarMorePrimitive,
  ActionBarPrimitive,
  AuiIf,
  BranchPickerPrimitive,
  ComposerPrimitive,
  ErrorPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  groupPartByType,
  useAui,
  useAuiState,
} from '@assistant-ui/react'
import { useAgUiState } from '@assistant-ui/react-ag-ui'
import { AssistantHero } from '@/components/assistant-ui/assistant-landing'
import { AssistantQuestions } from '@/components/assistant-ui/assistant-questions'
import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachmentChips,
  UserMessageAttachments,
  UserMessagePartText,
} from '@/components/assistant-ui/attachment'
import {
  useAssistantHydrating,
  useAssistantInterruptForSend,
  useAssistantTurnLive,
} from '@/components/assistant-ui/backend-port'
import { gateway } from '@/components/assistant-ui/gw/client'
import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import { ModelSelect } from '@/components/assistant-ui/model-select'
import {
  Reasoning,
  ReasoningContent,
  ReasoningRoot,
  ReasoningText,
  ReasoningTrigger,
} from '@/components/assistant-ui/reasoning'
import {
  readAttachmentsForCarryOver,
  useAssistantReconnect,
  useCreateAssistantSession,
} from '@/components/assistant-ui/runtime-provider'
import { ToolFallback } from '@/components/assistant-ui/tool-fallback'
import {
  ToolGroupContent,
  ToolGroupRoot,
  ToolGroupTrigger,
} from '@/components/assistant-ui/tool-group'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Switch } from '@/components/ui/switch'
import { useTranslation } from '@/lib/i18n'
import {
  atomAssistantAutoOpenPage,
  atomAssistantPendingAttachment,
  atomAssistantPendingAutoSend,
  atomAssistantStreamStuck,
  atomAssistantTopicNudge,
  atomAssistantTopicNudgeEnabled,
  atomAssistantTopicVerdicts,
  atomAssistantUserKey,
} from '@/lib/assistant/store'
import { cn } from '@/lib/utils'
import { parsePageMarker } from '@/lib/assistant/page-context'
import { useAtom, useAtomValue, useSetAtom } from 'jotai'
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  CornerDownLeftIcon,
  DownloadIcon,
  Loader2Icon,
  MessageSquarePlusIcon,
  MoreHorizontalIcon,
  SlidersHorizontalIcon,
  SquareIcon,
  WifiOffIcon,
} from 'lucide-react'
import { type FC, type ReactNode, useEffect, useRef, useState } from 'react'

/**
 * The conversation.
 *
 * Its empty state has two shapes, and which one applies is decided by whether a
 * `landing` was given:
 *
 *   * WITHOUT one (the side panel, or a page with no cluster to report on) the
 *     greeting floats and the composer sits at the bottom — the same place it will
 *     be for the rest of the conversation, so sending the first message moves
 *     nothing.
 *   * WITH one the whole screen scrolls: greeting, composer, then the suggestions
 *     and the cluster board below it. The composer cannot be sticky here — pinned
 *     to the viewport bottom it would hover over the board as you scrolled past
 *     it — so it rides in the flow until the first message, at which point the
 *     landing is gone and the sticky footer takes over.
 */
export const Thread: FC<{ landing?: ReactNode; readOnly?: boolean }> = ({
  landing,
  readOnly,
}) => {
  const { t } = useTranslation()
  const isEmpty = useAuiState(s => s.thread.isEmpty)
  // An empty runtime means two completely different things, and only the port
  // can tell them apart: a NEW conversation (show the landing) or one whose
  // transcript is still in flight (show nothing but a spinner). Without the
  // second case every switch between two history conversations flashed the
  // greeting and the cluster board — a landing screen for a conversation that
  // has been going for twenty turns.
  const hydrating = useAssistantHydrating()
  const inFlow = isEmpty && !!landing && !hydrating
  const column = useRef<HTMLDivElement>(null)
  // Which conversation this is, so the effect below fires once per switch
  // rather than once per transition into the empty state.
  const threadKey = useAuiState(
    s => s.threadListItem?.remoteId ?? s.threadListItem?.externalId
  )

  // A new conversation starts at the TOP of its landing.
  //
  // Two things were putting it at the bottom. The viewport is one scroller
  // shared by every conversation, so it keeps whatever offset the last one left
  // behind — and the library then scrolls it to the bottom on every switch
  // (`scrollToBottomOnThreadSwitch`, turned off below). On a landing, "the
  // bottom" is the far end of the cluster board, so clicking New dropped the
  // user into a screen of status cards with the greeting and the composer
  // somewhere above them.
  //
  // Keyed on the conversation, not just on entering the landing: New from a
  // landing that is already scrolled down leaves `inFlow` true throughout, and
  // that is precisely the case where the stale offset shows.
  useEffect(() => {
    if (!inFlow) return
    const viewport = column.current?.closest(
      '[data-slot="aui_thread-viewport"]'
    )
    if (!viewport) return
    // Cancel any stick-to-bottom the viewport is still holding, or it undoes
    // this the moment the board grows. It latches one when a transcript loads
    // and only lets go once a scroll actually lands at a bottom BELOW the fold
    // — so a short conversation (nothing to scroll) leaves it armed, and the
    // next tall thing rendered into the same scroller gets yanked to its end.
    // That tall thing is the landing. `pointerdown` on the viewport is the
    // library's own release for this, and non-bubbling keeps it from reading as
    // a click anywhere else.
    viewport.dispatchEvent(new Event('pointerdown', { bubbles: false }))
    // `instant` beats the viewport's `scroll-smooth`, which would otherwise
    // animate the whole board past the user on the way up.
    viewport.scrollTo({ top: 0, behavior: 'instant' })
  }, [inFlow, threadKey])

  // Read-only is subtractive, and everything subtracted here is a way to SPEAK:
  // the composer, the suggested follow-ups, the stream-health alert (about a run
  // nobody started) and the topic-switch nudge (which offers to move a message
  // into a new conversation). What remains is the transcript and the means to
  // scroll it. Disabling the composer instead would have left a text field
  // inviting input that goes nowhere.
  const footer = readOnly ? (
    <ThreadScrollToBottom />
  ) : (
    <>
      {/* Only where there is a conversation to scroll back down to. On the
          landing the viewport scrolls past the composer to the board below, so
          the button would appear over an empty screen offering to return to
          nothing. */}
      {!inFlow && <ThreadScrollToBottom />}
      <AssistantQuestions />
      <StreamHealthAlert />
      <TruncatedReplayNotice />
      <TopicSwitchNudge />
      <Composer />
    </>
  )
  return (
    <ThreadPrimitive.Root
      // `flex-1 min-h-0`, not `h-full` alone: the thread is a flex child sitting
      // BELOW the surface's header row(s), so `height:100%` makes it as tall as
      // the whole surface and pushes its own bottom — the composer — that many
      // pixels out of view (52px on the page, 104px in the panel, which also has
      // a title strip). `flex-1` sizes it to what is actually left, `min-h-0`
      // lets it shrink below its content, and `h-full` stays as the fallback for
      // a non-flex parent.
      className="aui-root aui-thread-root @container flex h-full min-h-0 flex-1 flex-col"
      style={{
        ['--thread-max-width' as string]: '44rem',
        ['--composer-radius' as string]: '24px',
        ['--composer-padding' as string]: '10px',
      }}
    >
      <ThreadPrimitive.Viewport
        turnAnchor="top"
        // The switch itself must not scroll. It fires BEFORE the runtime knows
        // what it switched to, so it cannot tell a conversation with history
        // from a brand-new one — and on the latter its scroll-to-bottom lands on
        // the far end of the landing board, then re-applies on every resize
        // while that board grows, overriding anything we set.
        //
        // Nothing is lost by turning it off: `scrollToBottomOnInitialize`
        // (still on) fires the moment a switched-to transcript arrives, which
        // is the case this was covering, and it knows there are messages.
        scrollToBottomOnThreadSwitch={false}
        data-slot="aui_thread-viewport"
        className="relative flex flex-1 flex-col overflow-x-auto overflow-y-scroll scroll-smooth"
      >
        <div
          ref={column}
          className="max-w-(--thread-max-width) mx-auto flex w-full flex-1 flex-col px-4 pt-4"
        >
          {hydrating ? (
            <div
              className="text-muted-foreground my-auto flex grow items-center justify-center"
              aria-busy
            >
              <Loader2Icon className="size-5 animate-spin" />
            </div>
          ) : (
            isEmpty &&
            // A greeting is an invitation to start; there is nothing to start
            // here, and an empty transcript is a fact about the conversation
            // being read rather than a blank page waiting for you.
            (readOnly ? (
              <div className="text-muted-foreground my-auto flex grow items-center justify-center text-sm">
                {t('assistant.thread.noMessages')}
              </div>
            ) : (
              <div
                className={cn(
                  'aui-thread-welcome-root flex flex-col',
                  inFlow ? 'pb-10 pt-16' : 'my-auto grow justify-center'
                )}
              >
                <AssistantHero />
              </div>
            ))
          )}

          <div
            data-slot="aui_message-group"
            className="mb-10 flex flex-col gap-y-8 empty:hidden"
          >
            <ThreadPrimitive.Messages>
              {() => <ThreadMessage />}
            </ThreadPrimitive.Messages>
          </div>

          {inFlow ? (
            <div className="relative flex flex-col gap-4">{footer}</div>
          ) : (
            /* OPAQUE, and explicitly so. `bg-inherit` looks right and is not:
               `background-color: inherit` takes the PARENT's computed value, and
               the parent here is an unpainted layout div — i.e. transparent. The
               conversation therefore scrolled visibly underneath the composer.
               The footer names the surface it sits on instead, and a short
               gradient above it keeps the text from being guillotined at the
               band's edge. */
            <ThreadPrimitive.ViewportFooter className="aui-thread-viewport-footer bg-card before:from-card sticky bottom-0 mt-auto flex flex-col gap-4 overflow-visible pb-4 before:pointer-events-none before:absolute before:inset-x-0 before:bottom-full before:h-8 before:bg-gradient-to-t before:to-transparent before:content-[''] md:pb-6">
              {footer}
            </ThreadPrimitive.ViewportFooter>
          )}
        </div>

        {/* The SAME column as the composer above it — same max width, same
            gutter — so the section headings and the suggestion pills start on
            the line the composer and the conversation start on. A landing a few
            rem wider than the thing it sits under reads as a misalignment, not
            as a wider board. The one part that genuinely needs the room, the
            status card grid, bleeds out of this column on its own (and only
            once the pane is wide enough to spend). */}
        {inFlow && (
          <div className="max-w-(--thread-max-width) mx-auto w-full px-4 pt-12">
            {landing}
          </div>
        )}
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  )
}

const ThreadMessage: FC = () => {
  const role = useAuiState(s => s.message.role)
  const isEditing = useAuiState(s => s.message.composer.isEditing)

  if (isEditing) return <EditComposer />
  if (role === 'user') return <UserMessage />
  return <AssistantMessage />
}

const ThreadScrollToBottom: FC = () => {
  const { t } = useTranslation()
  return (
    <ThreadPrimitive.ScrollToBottom
      render={
        <TooltipIconButton
          tooltip={t('assistant.scrollToBottom')}
          variant="outline"
          className="aui-thread-scroll-to-bottom dark:border-border dark:bg-background dark:hover:bg-accent absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible"
        />
      }
    >
      <ArrowDownIcon />
    </ThreadPrimitive.ScrollToBottom>
  )
}

/** Recovery banner shown when a sent prompt produced no streamed response within
 *  the watchdog window — the `/event` stream is presumed silently dead (see
 *  StreamHealthBridge). Offers a manual reconnect (re-syncs in place, recovers a
 *  response already produced server-side) and a page refresh as the last resort. */
const StreamHealthAlert: FC = () => {
  const { t } = useTranslation()
  const stuck = useAtomValue(atomAssistantStreamStuck)
  const setStuck = useSetAtom(atomAssistantStreamStuck)
  const reconnect = useAssistantReconnect()
  if (!stuck) return null
  return (
    <Alert variant="destructive">
      <WifiOffIcon />
      <AlertTitle>{t('assistant.stream.stuckTitle')}</AlertTitle>
      <AlertDescription>{t('assistant.stream.stuckBody')}</AlertDescription>
      <div className="col-start-2 mt-2 flex gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => {
            reconnect()
            setStuck(false)
          }}
        >
          {t('assistant.stream.retry')}
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={() => window.location.reload()}
        >
          {t('assistant.stream.refresh')}
        </Button>
      </div>
    </Alert>
  )
}

/** How much of the message the nudge quotes back. Long enough to recognise which
 *  send it is, short enough not to become a second copy of the message. */
const TOPIC_NUDGE_QUOTE_CHARS = 80

/** First `max` characters on one line, ellipsised. Newlines collapse to spaces so
 *  a multi-line message still quotes as a single readable fragment. */
function excerpt(text: string, max: number): string {
  const flat = text.replace(/\s+/g, ' ').trim()
  return flat.length <= max ? flat : `${flat.slice(0, max)}…`
}

/** Non-blocking nudge shown after a sent message the classifier judged to start a
 *  new topic (see TopicNudgeBridge). The send already happened — this only offers
 *  to MOVE it: abort the current run, branch to a fresh session, and re-send the
 *  same message there (via atomAssistantPendingAutoSend + ComposerAutoSendBridge).
 *  Dismissible; never interrupts the current reply on its own. */
const TopicSwitchNudge: FC = () => {
  const { t } = useTranslation()
  const [nudge, setNudge] = useAtom(atomAssistantTopicNudge)
  const setAutoSend = useSetAtom(atomAssistantPendingAutoSend)
  const setPendingAttachments = useSetAtom(atomAssistantPendingAttachment)
  const userKey = useAtomValue(atomAssistantUserKey)
  const { create } = useCreateAssistantSession()
  const sessionId = useAuiState(
    s => s.threadListItem?.externalId ?? s.threadListItem?.remoteId
  )
  const isEmpty = useAuiState(s => s.thread.isEmpty)

  if (!nudge) return null
  // Only for the conversation that raised it. The atom is a singleton while this
  // component renders in EVERY Thread footer — panel, full page, and the empty
  // landing — so an un-owned nudge trailed the user out of the conversation it
  // was about and offered to move a message that is not on screen.
  //
  // Two guards, because they cover different things. A nudge always belongs to a
  // SEND, so an empty thread can never be its owner: that alone settles the
  // landing, which is the loud case (the greeting sat under an alert quoting
  // some earlier session). The id check then stops it leaking between two
  // conversations that both have history — and stays deliberately lenient, since
  // a backend without the thread-list capability has no id here at all and must
  // not lose the feature over it.
  if (isEmpty) return null
  if (nudge.sessionId && sessionId && nudge.sessionId !== sessionId) return null

  const onMove = async () => {
    const text = nudge.newInput
    setNudge(null)
    // The moved message's attachments come along, and they have to be re-STAGED:
    // the new session gets its own sandbox, where the old marker's path is not a
    // file. Read the pod-side staged bytes while the old session is still the
    // current one; re-attaching them in the new thread re-stages them there.
    const carried = await readAttachmentsForCarryOver(
      sessionId,
      nudge.attachments ?? []
    )
    // Stop the reply already streaming in the current thread, then branch. The
    // gateway owns the interrupt for every backend, so this no longer reaches
    // into a harness client.
    if (sessionId && userKey) {
      try {
        await gateway.interrupt(userKey, sessionId)
      } catch {
        // best-effort — proceed to branch regardless
      }
    }
    try {
      await create()
    } catch {
      return
    }
    // Queue AFTER the switch so the bridges fire in the NEW (empty) thread, not
    // the one we just left. Attachments first: the auto-send bridge waits for
    // them to land in the composer before it sends.
    if (carried.length) setPendingAttachments(carried)
    setAutoSend(text)
  }

  return (
    <Alert>
      <MessageSquarePlusIcon />
      <AlertTitle>{t('assistant.topicNudgeTitle')}</AlertTitle>
      <AlertDescription className="flex flex-col gap-1">
        {/* Which message this is about. A reply is usually streaming by now and
            the composer has been cleared, so without the quote the user has to
            guess which of their sends the classifier reacted to. */}
        <span className="line-clamp-2 italic">
          {t('assistant.topicNudgeQuoted', {
            question: excerpt(nudge.newInput, TOPIC_NUDGE_QUOTE_CHARS),
          })}
        </span>
        <span>{t('assistant.topicNudgeBody')}</span>
      </AlertDescription>
      <div className="col-start-2 mt-2 flex gap-2">
        <Button type="button" size="sm" onClick={onMove}>
          {t('assistant.topicNudgeMove')}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => setNudge(null)}
        >
          {t('assistant.topicNudgeDismiss')}
        </Button>
      </div>
    </Alert>
  )
}

// Pasted text longer than this is converted into an attachment instead of being
// dropped into the input — so a big paste (a whole YAML / log) goes through the
// sandbox attachment path and doesn't bloat the composer or the context. Short
// snippets paste inline as usual. (Proposal 0055; cf. Gemini.)
const PASTE_ATTACH_THRESHOLD = 2000

const Composer: FC = () => {
  const { t } = useTranslation()
  const aui = useAui()
  // Two different questions. On an empty screen the composer is an invitation;
  // in a running conversation it is the next step.
  const isEmpty = useAuiState(s => s.thread.isEmpty)
  const { submit } = useComposerSubmit()

  // Enter is ours, which is why the primitive is told not to claim it.
  //
  // assistant-ui swallows Enter outright while the thread is running unless the
  // runtime declares a message queue, and the AG-UI runtime declares none — so
  // left to the primitive, pressing Enter mid-turn did nothing at all. Taking the
  // key means reproducing the two things the primitive got right: Shift+Enter is
  // a newline, and a keystroke that is still composing (every IME, so every
  // Chinese sentence) is not a send.
  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter' || e.shiftKey) return
    if (e.nativeEvent.isComposing) return
    e.preventDefault()
    void submit()
  }

  const handlePaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    // Real files → let assistant-ui's built-in file-paste handler run.
    if (e.clipboardData.files?.length) return
    const text = e.clipboardData.getData('text/plain')
    if (text.length <= PASTE_ATTACH_THRESHOLD) return
    e.preventDefault()
    const file = new File([text], 'pasted.txt', { type: 'text/plain' })
    void aui.composer().addAttachment(file)
  }

  return (
    <ComposerPrimitive.Root className="aui-composer-root relative flex w-full flex-col">
      <ComposerPrimitive.AttachmentDropzone
        render={
          <div
            data-slot="aui_composer-shell"
            // The composer rings on `focus-within` (the focus lives on the
            // textarea inside), which the global `:focus-visible` halo rule
            // deliberately does not cover — so it names the halo token directly
            // rather than drifting to shadcn's heavier default.
            className="bg-card focus-within:border-ring/75 data-[dragging=true]:border-ring data-[dragging=true]:bg-accent/50 rounded-(--composer-radius) p-(--composer-padding) flex w-full flex-col gap-2 border transition-shadow focus-within:ring-2 focus-within:ring-[color:var(--ring-halo)] data-[dragging=true]:border-dashed"
          />
        }
      >
        <ComposerAttachments />
        <ComposerPrimitive.Input
          placeholder={
            isEmpty
              ? t('assistant.composerPlaceholderEmpty')
              : t('assistant.composerPlaceholder')
          }
          className="aui-composer-input placeholder:text-muted-foreground/80 px-1.75 max-h-32 min-h-10 w-full resize-none bg-transparent py-1 text-sm outline-none"
          rows={1}
          autoFocus
          aria-label="Message input"
          submitMode="none"
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
        />
        <ComposerAction />
      </ComposerPrimitive.AttachmentDropzone>
    </ComposerPrimitive.Root>
  )
}

/**
 * Says so when a rejoined turn could not be shown from its beginning.
 *
 * The gateway buffers a detached turn's output within a bound; a turn that ran for
 * a long time with nobody watching can overflow it. Showing the surviving part
 * silently would present a partial answer as the whole one, which is the failure
 * mode this whole reconnect path exists to avoid.
 */
const TruncatedReplayNotice: FC = () => {
  const { t } = useTranslation()
  const dropped = useAgUiState<{ dropped?: number }>()?.dropped
  if (!dropped) return null
  return (
    <div className="text-muted-foreground px-1 pb-1 text-xs">
      {t('assistant.replayTruncated')}
    </div>
  )
}

/**
 * The one way a message leaves this composer, however it was triggered.
 *
 * A message cannot join a turn that is already running. Neither harness offers a
 * way to do that which keeps the transcript honest, and AG-UI has no way to put a
 * user message inside a run at all — so rather than pretend, the composer ASKS:
 * confirming ends the turn in flight and sends the message as its own turn.
 *
 * The question is keyed on `turnLive` — does the CONVERSATION have a turn — and not
 * on `thread.isRunning`, which only says whether this tab is holding the stream.
 * The two disagree exactly when a tab has lost touch with a conversation that is
 * still working, and a composer that keys off the tab's answer sends straight into
 * a running turn. If the send races one anyway (another tab, or a turn that starts
 * in between), `GatewayAgent` rejoins that turn and re-runs the message after it,
 * so no path here ends in an error the user has to read.
 */
function useComposerSubmit() {
  const aui = useAui()
  const interruptForSend = useAssistantInterruptForSend()
  const turnLive = useAssistantTurnLive()
  const isRunning = useAuiState(s => s.thread.isRunning)
  const text = useAuiState(s => s.composer.text)
  const canSend = useAuiState(s => s.composer.canSend)
  const [confirming, setConfirming] = useState(false)
  const [sending, setSending] = useState(false)
  /** The agent is working, on this tab's stream or on another's. */
  const busy = turnLive || isRunning

  const submit = () => {
    if (!text.trim() || sending) return
    if (busy) {
      setConfirming(true)
      return
    }
    aui.composer().send()
  }

  /** Confirmed: stop what is running, then send. Awaiting the interrupt matters —
   *  a send issued while the old run is still draining overlaps two runs on one
   *  thread, which the gateway rejects. */
  const confirmAndSend = async () => {
    setConfirming(false)
    setSending(true)
    try {
      await interruptForSend?.()
      // Read again rather than captured: the dialog sat between the keystroke and
      // here, and the composer still holds whatever the user last typed.
      if (aui.composer().getState().text.trim()) aui.composer().send()
    } finally {
      setSending(false)
    }
  }

  return {
    submit,
    sending,
    canSend: canSend && !sending,
    busy,
    confirming,
    setConfirming,
    confirmAndSend,
  }
}

/**
 * Send. One button whether or not the agent is working — what changes is that a
 * send into a running conversation asks first, and the question is what carries
 * the consequence (the turn in flight ends).
 */
const ComposerSend: FC = () => {
  const { t } = useTranslation()
  const {
    submit,
    sending,
    canSend,
    busy,
    confirming,
    setConfirming,
    confirmAndSend,
  } = useComposerSubmit()
  const label = busy
    ? t('assistant.interruptAndSend')
    : t('assistant.sendMessage')
  return (
    <>
      <TooltipIconButton
        tooltip={label}
        side="bottom"
        type="button"
        variant="default"
        size="icon"
        className="aui-composer-send size-8 rounded-full"
        aria-label={label}
        disabled={!canSend}
        onClick={() => submit()}
      >
        {busy ? (
          <CornerDownLeftIcon className="size-4" />
        ) : (
          <ArrowUpIcon className="aui-composer-send-icon size-4" />
        )}
      </TooltipIconButton>
      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('assistant.interruptTitle')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t('assistant.interruptBody')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={sending}>
                {t('common.cancel')}
              </AlertDialogCancel>
              <AlertDialogAction onClick={() => void confirmAndSend()}>
                {t('assistant.interruptConfirm')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

const ComposerAction: FC = () => {
  return (
    <div className="aui-composer-action-wrapper relative flex items-center justify-between gap-2">
      {/* Left: tools menu then model picker (AI-Studio style). Right:
          add-attachment then send. The left group is allowed to shrink
          (min-w-0 → the model name truncates) while the right group is pinned
          (shrink-0) so the send button never gets pushed out of the composer
          when the sidebar is narrow. */}
      <div className="flex min-w-0 items-center gap-1">
        <ComposerToolsMenu />
        <ModelSelect />
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <ComposerAddAttachment />
        {/* Never hidden while the agent works: a composer that takes the message
            and shows it pending is the whole point, and `ComposerPrimitive.Send`
            cannot be used for it — the library disables it during a run unless
            the runtime declares a message queue, which the AG-UI one does not. */}
        <ComposerSend />
        <AuiIf condition={s => s.thread.isRunning}>
          <ComposerPrimitive.Cancel
            render={
              <Button
                type="button"
                variant="default"
                size="icon"
                className="aui-composer-cancel size-8 rounded-full"
                aria-label="Stop generating"
              />
            }
          >
            <SquareIcon className="aui-composer-cancel-icon size-3 fill-current" />
          </ComposerPrimitive.Cancel>
        </AuiIf>
      </div>
    </div>
  )
}

/** "Tools" dropdown on the composer's left. Holds runtime toggles that change
 *  how the agent's actions affect the UI — currently the auto-open-page switch
 *  that gates whether an `open_page` tool call navigates the dashboard. */
const ComposerToolsMenu: FC = () => {
  const { t } = useTranslation()
  const [autoOpenPage, setAutoOpenPage] = useAtom(atomAssistantAutoOpenPage)
  const [topicNudge, setTopicNudge] = useAtom(atomAssistantTopicNudgeEnabled)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="aui-composer-tools text-muted-foreground hover:text-foreground h-8 shrink-0 gap-1.5 rounded-full px-2.5"
            aria-label={t('assistant.tools')}
          />
        }
      >
        <SlidersHorizontalIcon className="size-4" />
        <span className="text-sm">{t('assistant.tools')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="w-60">
        {/* GroupLabel must sit inside a Group (base-ui) — wrap both. */}
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t('assistant.tools')}</DropdownMenuLabel>
          <DropdownMenuItem
            // Keep the menu open when toggling so the user sees the state flip.
            closeOnClick={false}
            onClick={() => setAutoOpenPage(v => !v)}
            className="flex items-center justify-between gap-2"
          >
            <span>{t('assistant.autoOpenPage')}</span>
            <Switch
              checked={autoOpenPage}
              // The row's onClick owns the toggle; stop the switch from
              // double-firing while keeping it a live control.
              onClick={e => e.stopPropagation()}
              onCheckedChange={setAutoOpenPage}
              aria-label={t('assistant.autoOpenPage')}
            />
          </DropdownMenuItem>
          <DropdownMenuItem
            closeOnClick={false}
            onClick={() => setTopicNudge(v => !v)}
            className="flex items-center justify-between gap-2"
          >
            <span>{t('assistant.topicNudge')}</span>
            <Switch
              checked={topicNudge}
              onClick={e => e.stopPropagation()}
              onCheckedChange={setTopicNudge}
              aria-label={t('assistant.topicNudge')}
            />
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const MessageError: FC = () => {
  return (
    <MessagePrimitive.Error>
      <ErrorPrimitive.Root className="aui-message-error-root border-destructive bg-destructive/10 text-destructive dark:bg-destructive/5 mt-2 rounded-md border p-3 text-sm dark:text-red-200">
        <ErrorPrimitive.Message className="aui-message-error-message line-clamp-2" />
      </ErrorPrimitive.Root>
    </MessagePrimitive.Error>
  )
}

const AssistantMessage: FC = () => {
  // reserves space for action bar and compensates with `-mb` for consistent msg spacing
  // keeps hovered action bar from shifting layout (autohide doesn't support absolute positioning well)
  // for pt-[n] use -mb-[n + 6] & min-h-[n + 6] to preserve compensation
  const ACTION_BAR_PT = 'pt-1.5'
  const ACTION_BAR_HEIGHT = `-mb-7.5 min-h-7.5 ${ACTION_BAR_PT}`

  return (
    <MessagePrimitive.Root
      data-slot="aui_assistant-message-root"
      data-role="assistant"
      className="fade-in slide-in-from-bottom-1 animate-in relative duration-150"
    >
      <div
        data-slot="aui_assistant-message-content"
        // ONE rhythm for the whole message: `gap-2` between every block, with the
        // same gap repeated inside the chain-of-thought group below, so reasoning
        // and tool groups sit evenly however they alternate. The blocks
        // themselves carry no vertical margin — adding one back re-creates the
        // uneven "16px after reasoning, 0 before it" stacking.
        // Prose is the exception, at double that gap (`mt-2` on either side of a
        // block), so a message reads as prose separated by machinery rather than
        // one continuous slab.
        // [contain-intrinsic-size:auto_24px] fixes issue #4104, don't change without checking for regressions
        className="text-foreground wrap-break-word flex flex-col gap-2 px-2 text-sm leading-relaxed [contain-intrinsic-size:auto_24px] [content-visibility:auto] [&>.aui-chain-of-thought+.aui-md]:mt-2 [&>.aui-md+.aui-chain-of-thought]:mt-2 [&>.aui-md+.aui-standalone-tool-call]:mt-2 [&>.aui-standalone-tool-call+.aui-md]:mt-2"
      >
        <MessagePrimitive.GroupedParts
          groupBy={groupPartByType({
            reasoning: ['group-chainOfThought', 'group-reasoning'],
            'tool-call': ['group-chainOfThought', 'group-tool'],
            // open_page opts into standalone display (display: 'standalone' in
            // group — always visible instead of hidden behind "N tool calls".
            'standalone-tool-call': [],
          })}
        >
          {({ part, children }) => {
            switch (part.type) {
              case 'group-chainOfThought':
                return (
                  <div
                    data-slot="aui_chain-of-thought"
                    className="aui-chain-of-thought flex flex-col gap-2"
                  >
                    {children}
                  </div>
                )
              case 'group-reasoning': {
                const running = part.status.type === 'running'
                return (
                  <ReasoningRoot defaultOpen={running}>
                    <ReasoningTrigger active={running} />
                    <ReasoningContent aria-busy={running}>
                      <ReasoningText>{children}</ReasoningText>
                    </ReasoningContent>
                  </ReasoningRoot>
                )
              }
              case 'group-tool':
                return (
                  <ToolGroupRoot>
                    <ToolGroupTrigger
                      count={part.indices.length}
                      active={part.status.type === 'running'}
                    />
                    <ToolGroupContent>{children}</ToolGroupContent>
                  </ToolGroupRoot>
                )
              case 'text':
                return <MarkdownText />
              case 'reasoning':
                return <Reasoning {...part} />
              case 'tool-call':
                return part.toolUI ?? <ToolFallback {...part} />
              default:
                return null
            }
          }}
        </MessagePrimitive.GroupedParts>
        <MessageError />
      </div>

      <div
        data-slot="aui_assistant-message-footer"
        className={cn('ms-2 flex items-center', ACTION_BAR_HEIGHT)}
      >
        <BranchPicker />
        <AssistantActionBar />
      </div>
    </MessagePrimitive.Root>
  )
}

/** Hover-reveal one-liner: model · duration. Rendered INSIDE the action bar so
 *  it shares the bar's autohide — appearing/hiding together with the Copy / More
 *  buttons (no separate hover flicker), and staying while More is open. Kept lean
 *  (no token count) so it fits the narrow side-panel; the full breakdown —
 *  tokens and cost included — lives in the More menu (MessageStatsDetails). The
 *  `min-w-0` lets the flex item shrink so `truncate` can clip a long model name. */
const AssistantMessageStats: FC = () => {
  const { model, durationMs } = useMessageStats()
  const parts = [model, formatDuration(durationMs)].filter(Boolean)
  if (parts.length === 0) return null
  return (
    <span className="ms-1 min-w-0 self-center truncate text-xs">
      {parts.join(' · ')}
    </span>
  )
}

const AssistantActionBar: FC = () => {
  const { t } = useTranslation()
  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      className="aui-assistant-action-bar-root text-muted-foreground col-start-3 row-start-2 -ms-1 flex gap-1"
    >
      <ActionBarPrimitive.Copy
        render={<TooltipIconButton tooltip={t('assistant.copy')} />}
      >
        <AuiIf condition={s => s.message.isCopied}>
          <CheckIcon />
        </AuiIf>
        <AuiIf condition={s => !s.message.isCopied}>
          <CopyIcon />
        </AuiIf>
      </ActionBarPrimitive.Copy>
      {/* No Reload: the opencode runtime maps onReload to session.revert, which
          rolls the session back rather than regenerating — not a useful refresh. */}
      <ActionBarMorePrimitive.Root>
        <ActionBarMorePrimitive.Trigger
          render={
            <TooltipIconButton
              tooltip={t('assistant.more')}
              className="data-[state=open]:bg-accent"
            />
          }
        >
          <MoreHorizontalIcon />
        </ActionBarMorePrimitive.Trigger>
        <ActionBarMorePrimitive.Content
          side="bottom"
          align="start"
          className="aui-action-bar-more-content bg-popover text-popover-foreground z-50 min-w-48 overflow-hidden rounded-md border p-1 shadow-md"
        >
          <MessageStatsDetails />
          <ActionBarPrimitive.ExportMarkdown
            render={
              <ActionBarMorePrimitive.Item className="aui-action-bar-more-item hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground flex cursor-pointer select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none" />
            }
          >
            <DownloadIcon className="size-4" />
            {t('assistant.exportMarkdown')}
          </ActionBarPrimitive.ExportMarkdown>
        </ActionBarMorePrimitive.Content>
      </ActionBarMorePrimitive.Root>
      <AssistantMessageStats />
    </ActionBarPrimitive.Root>
  )
}

const UserMessage: FC = () => {
  return (
    <MessagePrimitive.Root
      data-slot="aui_user-message-root"
      className="fade-in slide-in-from-bottom-1 animate-in group grid auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] content-start gap-y-2 px-2 duration-150 [contain-intrinsic-size:auto_60px] [content-visibility:auto] [&:where(>*)]:col-start-2"
      data-role="user"
    >
      {/* Live attachments carry an `attachments` array; round-tripped ones come
          back as `<attachment …>` text parts — render a downloadable chip for
          each, above the bubble. */}
      <UserMessageAttachments />
      <UserMessageAttachmentChips />

      <div className="aui-user-message-content-wrapper col-start-2 min-w-0">
        {/* No edit affordance: the opencode runtime provides no `onEdit`, so
            assistant-ui's edit capability is off and the button would be a
            dead no-op. Reload/branch still work on assistant messages. */}
        {/* One step OFF the thread's surface, not onto it. The conversation is
            painted `--card`, so a `bg-card` bubble was the same white as the
            page behind it and only its hairline said anything was there;
            `--background` is the canvas tone, which reads as a bubble on a
            panel. (In dark mode the pair is inverted and the contrast holds.) */}
        <div className="aui-user-message-content bg-background border-border text-foreground wrap-break-word rounded-2xl border px-4 py-2.5 text-sm empty:hidden">
          {/* Hide the raw `<attachment …>` text in the bubble (it's the chip
              above); normal text renders as Markdown. */}
          <MessagePrimitive.Parts components={{ Text: UserMessagePartText }} />
        </div>
      </div>

      <UserActionBar />

      <BranchPicker
        data-slot="aui_user-branch-picker"
        className="col-span-full col-start-1 row-start-3 -me-1 justify-end"
      />
    </MessagePrimitive.Root>
  )
}

/** Reveal-on-hover bar under a user bubble, like the assistant's.
 *
 *  Hidden by opacity rather than by `autohide`, which UNMOUNTS the bar: the row
 *  would collapse and the whole conversation would jump every time the pointer
 *  crossed a message. This keeps the row's height and only fades its contents.
 *
 *  It also stays up while its More menu is open — the menu content is portalled,
 *  so without the `has-` rule moving the pointer to the menu would hide the
 *  trigger underneath it. `focus-within` is the keyboard equivalent of hovering. */
const UserActionBar: FC = () => {
  const { t } = useTranslation()
  return (
    <ActionBarPrimitive.Root
      autohide="never"
      className="aui-user-action-bar-root text-muted-foreground col-start-2 -me-1 flex justify-end gap-1 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100 has-[[data-state=open]]:opacity-100"
    >
      <ActionBarPrimitive.Copy
        render={<TooltipIconButton tooltip={t('assistant.copy')} />}
      >
        <AuiIf condition={s => s.message.isCopied}>
          <CheckIcon />
        </AuiIf>
        <AuiIf condition={s => !s.message.isCopied}>
          <CopyIcon />
        </AuiIf>
      </ActionBarPrimitive.Copy>
      <MessageContextMenu />
    </ActionBarPrimitive.Root>
  )
}

/** What the system knows about THIS user message, in its More menu: the page the
 *  user was on when they sent it, and the topic classifier's read on it.
 *
 *  The menu used to render nothing at all when there was no verdict, which made
 *  three very different situations look identical: the classifier is off, this is
 *  the session's first message (never classified, by design), or something broke.
 *  It now always opens and says which one — the absence of a verdict is
 *  information, and hiding it only produced "why is there nothing here?".
 *
 *  Both classifier axes are shown, not just the outcome: a new topic requires
 *  BOTH to have changed, so "same object" alone explains a continuation that
 *  would otherwise look wrong. */
const MessageContextMenu: FC = () => {
  const { t } = useTranslation()
  const messageId = useAuiState(s => s.message.id)
  const classifierOn = useAtomValue(atomAssistantTopicNudgeEnabled)
  const isFirst = useAuiState(s => s.message.index === 0)
  const verdicts = useAtomValue(atomAssistantTopicVerdicts)
  const verdict = messageId ? verdicts[messageId] : undefined
  // The page marker the dashboard appended when this message was sent. Read back
  // off the message itself, so it stays accurate for an old message in a reloaded
  // session rather than reflecting wherever the user has since navigated.
  const page = parsePageMarker(
    useAuiState(s =>
      s.message.parts
        .flatMap(p => (p.type === 'text' ? [p.text] : []))
        .join('\n')
    )
  )

  const carry = (v?: boolean) =>
    v === undefined
      ? '—'
      : v
        ? t('assistant.topicVerdict.carriedOver')
        : t('assistant.topicVerdict.changed')
  const pageRows: Array<[string, string]> = page
    ? [
        // A marker off an uncatalogued route carries the cluster only — show the
        // rows it actually has rather than an empty "page: ".
        ...(page.key
          ? ([[t('assistant.pageContext.page'), page.key]] as Array<
              [string, string]
            >)
          : []),
        ...(page.cluster
          ? ([[t('assistant.pageContext.cluster'), page.cluster]] as Array<
              [string, string]
            >)
          : []),
        ...Object.entries(page.params ?? {}),
      ]
    : []
  const verdictRows: Array<[string, string]> = verdict
    ? [
        [
          t('assistant.topicVerdict.outcome'),
          verdict.isNewTopic
            ? t('assistant.topicVerdict.newTopic')
            : t('assistant.topicVerdict.continuation'),
        ],
        [t('assistant.topicVerdict.object'), carry(verdict.objectCarriesOver)],
        [t('assistant.topicVerdict.goal'), carry(verdict.goalCarriesOver)],
        [
          t('assistant.topicVerdict.confidence'),
          `${Math.round((verdict.confidence ?? 0) * 100)}%`,
        ],
      ]
    : []
  // Why there is no verdict, in the order the reasons actually apply.
  const noVerdictReason = !classifierOn
    ? t('assistant.topicVerdict.disabled')
    : isFirst
      ? t('assistant.topicVerdict.firstMessage')
      : t('assistant.topicVerdict.pending')

  return (
    <ActionBarMorePrimitive.Root>
      <ActionBarMorePrimitive.Trigger
        render={
          <TooltipIconButton
            tooltip={t('assistant.messageContext.title')}
            className="data-[state=open]:bg-accent"
          />
        }
      >
        <MoreHorizontalIcon />
      </ActionBarMorePrimitive.Trigger>
      <ActionBarMorePrimitive.Content
        side="bottom"
        align="end"
        className="aui-action-bar-more-content bg-popover text-popover-foreground z-50 min-w-56 max-w-72 overflow-hidden rounded-md border p-1 shadow-md"
      >
        <MessageStatsDetails />
        {pageRows.length > 0 && (
          <MessageContextSection
            title={t('assistant.pageContext.title')}
            rows={pageRows}
          />
        )}
        {verdictRows.length > 0 ? (
          <MessageContextSection
            title={t('assistant.topicVerdict.title')}
            rows={verdictRows}
            note={verdict?.rationale}
          />
        ) : (
          <MessageContextSection
            title={t('assistant.topicVerdict.title')}
            rows={[]}
            note={noVerdictReason}
          />
        )}
      </ActionBarMorePrimitive.Content>
    </ActionBarMorePrimitive.Root>
  )
}

/** One labelled block of key/value rows in the message context menu, with an
 *  optional trailing note (the classifier's rationale, or why it has none). */
const MessageContextSection: FC<{
  title: string
  rows: Array<[string, string]>
  note?: string
}> = ({ title, rows, note }) => (
  <>
    <div className="text-muted-foreground px-2 pb-1 pt-1.5 text-xs font-medium">
      {title}
    </div>
    {rows.length > 0 && (
      <div className="px-2 pb-1.5">
        {rows.map(([k, v]) => (
          <div key={k} className="flex justify-between gap-4 text-xs leading-5">
            <span className="text-muted-foreground">{k}</span>
            <span className="wrap-break-word tabular-nums">{v}</span>
          </div>
        ))}
      </div>
    )}
    {note && (
      <p className="text-muted-foreground wrap-break-word px-2 pb-1.5 text-xs italic leading-4">
        {note}
      </p>
    )}
  </>
)

// --- Per-message stats (model / timing / tokens / cost) ---
// Per-turn statistics come from two places, and both are needed.
//
// LIVE: the gateway reports them as AG-UI agent state keyed by assistant message
// id (`useAgUiState`). Not as message metadata — the AG-UI runtime builds that
// itself and leaves no seam for ours.
// REPLAYED: on a thread switch we hydrate messages ourselves, so there the same
// figures are stamped into `metadata.custom.navix` (see the gateway runtime's
// `transcriptToMessages`). Reading only one of the two loses the model name and
// token counts either live or after a reload.
//
// `metadata.timing` stays the library's: it measures the stream we just watched,
// which is something only the runtime can know.
interface NavixMessageStats {
  model?: string
  costUsd?: number
  usage?: {
    inputTokens?: number
    outputTokens?: number
    cacheReadTokens?: number
    cacheCreationTokens?: number
  }
}

interface MessageStats {
  createdAt?: Date
  model?: string
  durationMs?: number
  tokensIn?: number
  tokensOut?: number
  cost?: number
}

function useMessageStats(): MessageStats {
  const createdAt = useAuiState(s => s.message.createdAt)
  const messageId = useAuiState(s => s.message.id)
  const custom = useAuiState(s => s.message.metadata.custom) as
    | { navix?: NavixMessageStats }
    | undefined
  const durationMs = useAuiState(
    s => s.message.metadata.timing?.totalStreamTime
  )
  // Safe outside an AG-UI runtime: the hook falls back to undefined rather than
  // requiring the provider.
  const live = useAgUiState<{
    stats?: Record<string, NavixMessageStats>
  }>()?.stats?.[messageId]

  const stats = live ?? custom?.navix
  return {
    createdAt,
    model: stats?.model,
    durationMs,
    tokensIn: stats?.usage?.inputTokens,
    tokensOut: stats?.usage?.outputTokens,
    cost: stats?.costUsd,
  }
}

function formatDuration(ms?: number): string | undefined {
  if (ms == null || ms < 0) return undefined
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  return `${Math.floor(s / 60)}m${Math.round(s % 60)}s`
}

function formatCost(n?: number): string | undefined {
  if (n == null) return undefined
  if (n === 0) return '$0'
  return `$${n.toFixed(n < 0.01 ? 4 : 2)}`
}

function formatClock(d?: Date): string | undefined {
  if (!d || Number.isNaN(d.getTime())) return undefined
  return d.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

/** Detailed stats block for the assistant "More" menu. Non-interactive rows;
 *  renders nothing when no stats are present. */
const MessageStatsDetails: FC = () => {
  const { t } = useTranslation()
  const { createdAt, model, durationMs, tokensIn, tokensOut, cost } =
    useMessageStats()

  const rows: Array<[string, string]> = []
  if (model) rows.push([t('assistant.stats.model'), model])
  const dur = formatDuration(durationMs)
  if (dur) rows.push([t('assistant.stats.duration'), dur])
  if (tokensIn != null || tokensOut != null) {
    rows.push([
      t('assistant.stats.tokens'),
      `${tokensIn ?? 0} → ${tokensOut ?? 0}`,
    ])
  }
  const c = formatCost(cost)
  if (c) rows.push([t('assistant.stats.cost'), c])
  const clock = formatClock(createdAt)
  if (clock) rows.push([t('assistant.stats.time'), clock])
  if (rows.length === 0) return null

  return (
    <>
      <div className="px-2 py-1.5">
        {rows.map(([k, v]) => (
          <div key={k} className="flex justify-between gap-4 text-xs leading-5">
            <span className="text-muted-foreground">{k}</span>
            <span className="tabular-nums">{v}</span>
          </div>
        ))}
      </div>
      <div className="bg-border -mx-1 my-1 h-px" />
    </>
  )
}

const EditComposer: FC = () => {
  return (
    <MessagePrimitive.Root
      data-slot="aui_edit-composer-wrapper"
      className="flex flex-col px-2"
    >
      <ComposerPrimitive.Root className="aui-edit-composer-root bg-background border-border ms-auto flex w-full max-w-[85%] flex-col rounded-2xl border">
        <ComposerPrimitive.Input
          className="aui-edit-composer-input text-foreground min-h-14 w-full resize-none bg-transparent p-4 text-sm outline-none"
          autoFocus
        />
        <div className="aui-edit-composer-footer mx-3 mb-3 flex items-center gap-2 self-end">
          <ComposerPrimitive.Cancel
            render={<Button variant="ghost" size="sm" />}
          >
            Cancel
          </ComposerPrimitive.Cancel>
          <ComposerPrimitive.Send render={<Button size="sm" />}>
            Update
          </ComposerPrimitive.Send>
        </div>
      </ComposerPrimitive.Root>
    </MessagePrimitive.Root>
  )
}

const BranchPicker: FC<BranchPickerPrimitive.Root.Props> = ({
  className,
  ...rest
}) => {
  const { t } = useTranslation()
  return (
    <BranchPickerPrimitive.Root
      hideWhenSingleBranch
      className={cn(
        'aui-branch-picker-root text-muted-foreground -ms-2 me-2 inline-flex items-center text-xs',
        className
      )}
      {...rest}
    >
      <BranchPickerPrimitive.Previous
        render={<TooltipIconButton tooltip={t('assistant.branchPrevious')} />}
      >
        <ChevronLeftIcon />
      </BranchPickerPrimitive.Previous>
      <span className="aui-branch-picker-state font-medium">
        <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
      </span>
      <BranchPickerPrimitive.Next
        render={<TooltipIconButton tooltip={t('assistant.branchNext')} />}
      >
        <ChevronRightIcon />
      </BranchPickerPrimitive.Next>
    </BranchPickerPrimitive.Root>
  )
}
