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

import {
  useAgUiInterrupts,
  useAgUiSubmitInterruptResponses,
} from '@assistant-ui/react-ag-ui'
import type { AgUiInterrupt, AgUiResumeEntry } from '@assistant-ui/react-ag-ui'
import { Button } from '@/components/ui/button'
import { useTranslation } from '@/lib/i18n'
import { useDecideApproval, type ApprovalScope } from '@/lib/queries/approval'
import { cn } from '@/lib/utils'
import { useCallback, useMemo, useState } from 'react'
import { toast } from 'sonner'

// Renders the agent's question / permission requests as selectable cards.
// Without this a question blocks the run forever.
//
// These are AG-UI INTERRUPTS: the run ends with `outcome.interrupt`, the card is
// read from `useAgUiInterrupts()`, and answering starts a new run carrying
// `resume` — which the gateway uses to settle the still-parked harness callback and
// rejoin the same turn. Two consequences shape this file:
//
//   * `submitInterruptResponses` refuses to send unless EVERY open interrupt has a
//     response, and throws before any network call. So the cards are submitted as
//     a GROUP: answers accumulate here and go out once, with anything untouched
//     reported as cancelled. Submitting one card at a time would silently reject
//     the others.
//   * The payload the gateway needs — the labelled options and the answer-map key
//     for each question — has no home in the protocol's `Interrupt`, so it rides in
//     `metadata.agentbox`. That is untrusted shaped-by-the-server data here, hence the
//     defensive read below rather than a cast.
export function AssistantQuestions() {
  const interrupts = useAgUiInterrupts()
  const cards = useMemo(
    () => interrupts.map(readCard).filter(Boolean),
    [interrupts]
  ) as Card[]
  if (!cards.length) return null
  return <InterruptQuestionList cards={cards} />
}

/** One interrupt, in the shape the card renders. */
interface Card {
  id: string
  request: RenderableQuestionRequest
  /** The answer-map key per question, positionally aligned with `questions`. The
   *  gateway keys answers by the question text as the agent phrased it; an
   *  index-keyed map would mis-assign as soon as the agent reorders them. */
  keys: string[]
  /** Present when the gate is holding a platform write. */
  approval?: ApprovalAsk
}

/** A held platform write, as the gateway describes it. Mirrors `ApprovalAsk` in
 *  the gateway's `agent-events.ts`; re-declared rather than imported because
 *  this side re-parses it defensively from an `unknown` protocol field. */
interface ApprovalAsk {
  approvalId: string
  cluster: string
  operation: string
  summary: string
  onceOnly: boolean
  command?: string
}

/** The tokens the gateway's tool understands. Never display text: the labels
 *  below are translated, and a locale must not be able to change what the agent
 *  does with the answer. */
const DECISION_APPROVE_ONCE = 'approve_once'
const DECISION_APPROVE_SESSION = 'approve_session'
const DECISION_DENY = 'deny'

function InterruptQuestionList({ cards }: { cards: Card[] }) {
  const submit = useAgUiSubmitInterruptResponses()
  const [answered, setAnswered] = useState<Record<string, string[][]>>({})

  // Fire once every card has been answered or declined. `undefined` in the map
  // means "still waiting on the user".
  const flush = useCallback(
    (next: Record<string, string[][]>) => {
      if (cards.some(c => next[c.id] === undefined)) {
        setAnswered(next)
        return
      }
      const responses: AgUiResumeEntry[] = cards.map(card => {
        const picked = next[card.id]
        const answers: Record<string, string> = {}
        picked.forEach((labels, i) => {
          const key = card.keys[i]
          // The gateway joins multi-select answers the same way, so this string
          // is what both harnesses already expect.
          if (key && labels.length) answers[key] = labels.join(', ')
        })
        return Object.keys(answers).length
          ? { interruptId: card.id, status: 'resolved', payload: { answers } }
          : // An empty answer map is how the backend learns the user declined;
            // the tool then reports "no answer" instead of hanging.
            { interruptId: card.id, status: 'cancelled' }
      })
      setAnswered({})
      void submit(responses).catch(e =>
        // A rejected submission would otherwise be indistinguishable from a hung
        // run.
        console.error('[assistant] answering the agent failed:', e)
      )
    },
    [cards, submit]
  )

  return (
    <div className="flex flex-col gap-2">
      {cards.map(card =>
        card.approval ? (
          <ApprovalCard
            key={card.id}
            approval={card.approval}
            onDecided={token => flush({ ...answered, [card.id]: [[token]] })}
          />
        ) : (
          <QuestionCard
            key={card.id}
            request={card.request}
            onReply={answers => flush({ ...answered, [card.id]: answers })}
            onReject={() => flush({ ...answered, [card.id]: [] })}
          />
        )
      )}
    </div>
  )
}

/**
 * `metadata.agentbox` → a renderable card.
 *
 * The namespace has to match `toInterrupt` in the gateway exactly. It did not:
 * the gateway sends `agentbox` and this read `navix`, inherited from the
 * implementation this was forked from. Nothing errored — `readCard` returned
 * null, the list came out empty and rendered nothing, so a run that had
 * correctly stopped to ask a question looked like a run that had simply carried
 * on. The `navix` key is still accepted so a card minted by a pod that has not
 * rolled yet is not lost mid-conversation.
 *
 * Defensive on purpose: this is server-shaped data reaching a component through an
 * `unknown`-typed protocol field, and a surprise shape must render nothing rather
 * than throw inside the thread.
 */
export function readCard(interrupt: AgUiInterrupt): Card | null {
  const meta = interrupt.metadata as
    | { agentbox?: unknown; navix?: unknown }
    | undefined
  const payload = meta?.agentbox ?? meta?.navix
  if (!payload || typeof payload !== 'object') return null
  const raw = (payload as { questions?: unknown }).questions
  if (!Array.isArray(raw)) return null
  const keys: string[] = []
  const questions: NonNullable<RenderableQuestionRequest['questions']> = []
  for (const q of raw) {
    if (!q || typeof q !== 'object') continue
    const item = q as {
      key?: unknown
      question?: unknown
      header?: unknown
      multiSelect?: unknown
      options?: unknown
    }
    if (typeof item.question !== 'string') continue
    keys.push(typeof item.key === 'string' ? item.key : item.question)
    questions.push({
      question: item.question,
      ...(typeof item.header === 'string' ? { header: item.header } : {}),
      options: Array.isArray(item.options)
        ? item.options.flatMap(o => {
            const opt = o as { label?: unknown; description?: unknown }
            return typeof opt?.label === 'string'
              ? [
                  {
                    label: opt.label,
                    ...(typeof opt.description === 'string'
                      ? { description: opt.description }
                      : {}),
                  },
                ]
              : []
          })
        : [],
      ...(item.multiSelect === true ? { multiple: true } : {}),
    })
  }
  if (!questions.length) return null
  const approval = readApproval((payload as { approval?: unknown }).approval)
  return {
    id: interrupt.id,
    request: { questions },
    keys,
    ...(approval ? { approval } : {}),
  }
}

/**
 * The approval block, or nothing.
 *
 * A partial one is treated as absent rather than repaired: without the id and
 * the cluster there is nowhere to send the decision, and a card that renders
 * approve buttons which cannot record anything is worse than the plain question
 * card this falls back to — that one at least resolves the interrupt.
 */
function readApproval(raw: unknown): ApprovalAsk | null {
  if (!raw || typeof raw !== 'object') return null
  const a = raw as Record<string, unknown>
  if (typeof a.approvalId !== 'string' || !a.approvalId) return null
  if (typeof a.cluster !== 'string' || !a.cluster) return null
  return {
    approvalId: a.approvalId,
    cluster: a.cluster,
    operation: typeof a.operation === 'string' ? a.operation : '',
    summary: typeof a.summary === 'string' ? a.summary : '',
    onceOnly: a.onceOnly === true,
    ...(typeof a.command === 'string' ? { command: a.command } : {}),
  }
}

/** What the card actually reads. Shared by every backend, since the gateway
 *  normalises both harnesses onto one question shape. */
interface RenderableQuestionRequest {
  questions?: {
    question: string
    header?: string
    options?: { label: string; description?: string }[]
    multiple?: boolean
    custom?: boolean
  }[]
}

function QuestionCard({
  request,
  onReply,
  onReject,
}: {
  request: RenderableQuestionRequest
  onReply: (answers: string[][]) => void
  onReject: () => void
}) {
  const { t } = useTranslation()
  const questions = request.questions ?? []
  const [selected, setSelected] = useState<Record<number, string[]>>({})
  const [custom, setCustom] = useState<Record<number, string>>({})

  // A question accepts free text when it explicitly allows custom input OR when
  // it ships no options at all (an open-ended ask, e.g. "what's the namespace
  // and name?"). Without this, an option-less question renders an unusable card
  // — just the prompt and a Cancel button, with no way to answer.
  const allowsText = (q: { custom?: boolean; options?: unknown[] }) =>
    !!q.custom || (q.options?.length ?? 0) === 0

  // One option-only question, single-select → click answers immediately.
  const oneClick =
    questions.length === 1 &&
    !questions[0].multiple &&
    !allowsText(questions[0]) &&
    (questions[0].options?.length ?? 0) > 0

  const answers = (): string[][] =>
    questions.map((_q, i) => {
      const sel = selected[i] ?? []
      const ct = (custom[i] ?? '').trim()
      return ct ? [...sel, ct] : sel
    })

  const ready = questions.every(
    (q, i) =>
      (selected[i]?.length ?? 0) > 0 ||
      (allowsText(q) && !!(custom[i] ?? '').trim())
  )

  const submit = () => {
    if (ready) onReply(answers())
  }

  const choose = (qi: number, label: string, multiple?: boolean) => {
    if (oneClick) {
      onReply([[label]])
      return
    }
    setSelected(prev => {
      const cur = prev[qi] ?? []
      if (multiple) {
        return {
          ...prev,
          [qi]: cur.includes(label)
            ? cur.filter(l => l !== label)
            : [...cur, label],
        }
      }
      return { ...prev, [qi]: [label] }
    })
  }

  return (
    <div className="bg-muted/40 rounded-md border p-3 text-sm">
      {questions.map((q, qi) => (
        <div key={qi} className="mb-3 last:mb-0">
          {q.header && (
            <div className="text-muted-foreground text-xs font-medium uppercase">
              {q.header}
            </div>
          )}
          <div className="mb-1.5 font-medium">{q.question}</div>
          <div className="flex flex-col gap-1.5">
            {(q.options ?? []).map(opt => {
              const isSel = (selected[qi] ?? []).includes(opt.label)
              return (
                <button
                  key={opt.label}
                  type="button"
                  onClick={() => choose(qi, opt.label, q.multiple)}
                  className={cn(
                    'hover:bg-accent rounded-md border px-2.5 py-1.5 text-left transition-colors',
                    isSel && 'border-primary bg-primary/10'
                  )}
                >
                  <div className="font-medium">{opt.label}</div>
                  {opt.description && (
                    <div className="text-muted-foreground text-xs">
                      {opt.description}
                    </div>
                  )}
                </button>
              )
            })}
            {allowsText(q) && (
              <input
                type="text"
                placeholder={t('assistant.answerPlaceholder')}
                value={custom[qi] ?? ''}
                onChange={e => setCustom(p => ({ ...p, [qi]: e.target.value }))}
                onKeyDown={e => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    submit()
                  }
                }}
                className="bg-background focus:border-ring rounded-md border px-2.5 py-1.5 text-sm outline-none"
              />
            )}
          </div>
        </div>
      ))}
      <div className="mt-2 flex items-center gap-2">
        {!oneClick && (
          <Button size="sm" className="h-7" disabled={!ready} onClick={submit}>
            {t('common.submit')}
          </Button>
        )}
        <Button size="sm" variant="ghost" className="h-7" onClick={onReject}>
          {t('common.cancel')}
        </Button>
      </div>
    </div>
  )
}

/**
 * A platform write the gate is holding, decided in the conversation.
 *
 * The decision is recorded on the PLATFORM first and the interrupt resolved only
 * after that succeeds. The order is the whole point: the gateway's tool re-runs
 * the command as soon as the interrupt settles, and a settle that raced ahead of
 * the grant would have it refused a second time — which reads to everyone
 * involved as the approve button not working.
 *
 * Recorded with the signed-in person's own session, never the agent's
 * credential. That is not a detail of this component, it is what the gate is
 * for: it refuses a decision made with the same unattended credential that
 * asked, so the browser is the only party that can say yes.
 */
function ApprovalCard({
  approval,
  onDecided,
}: {
  approval: ApprovalAsk
  onDecided: (token: string) => void
}) {
  const { t } = useTranslation()
  const decide = useDecideApproval(approval.cluster)
  const [busy, setBusy] = useState<string | null>(null)

  const send = (
    decision: 'approve' | 'deny',
    scope: ApprovalScope,
    token: string
  ) => {
    setBusy(token)
    decide.mutate(
      { id: approval.approvalId, decision, scope },
      {
        onSuccess: () => {
          toast.success(
            decision === 'approve'
              ? t('approvals.toast.approved')
              : t('approvals.toast.denied')
          )
          onDecided(token)
        },
        onError: () => {
          // Left unresolved on purpose. The agent is still parked, so the person
          // can press the button again — resolving it here would hand the agent
          // an approval that was never recorded.
          setBusy(null)
          toast.error(t('approvals.toast.failed'))
        },
      }
    )
  }

  return (
    <div className="bg-muted/40 rounded-md border p-3 text-sm">
      <div className="text-muted-foreground text-xs font-medium uppercase">
        {t('assistant.approval.title')}
      </div>
      <div className="mb-1.5 font-medium">
        {approval.summary || approval.operation}
      </div>
      {approval.command && (
        <div className="bg-background text-muted-foreground mb-2 overflow-x-auto rounded-md border px-2.5 py-1.5 font-mono text-xs">
          {approval.command}
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <button
          type="button"
          disabled={!!busy}
          onClick={() => send('approve', 'once', DECISION_APPROVE_ONCE)}
          className="hover:bg-accent rounded-md border px-2.5 py-1.5 text-left transition-colors disabled:opacity-50"
        >
          <div className="font-medium">{t('approvals.action.once')}</div>
        </button>
        {/* Destructive and credential-minting operations are never granted more
            widely than one call, and the platform would refuse the wider scope
            anyway — offering a button that 400s is worse than offering none. */}
        {!approval.onceOnly && (
          <button
            type="button"
            disabled={!!busy}
            onClick={() => send('approve', 'session', DECISION_APPROVE_SESSION)}
            className="hover:bg-accent rounded-md border px-2.5 py-1.5 text-left transition-colors disabled:opacity-50"
          >
            <div className="font-medium">{t('approvals.action.session')}</div>
            <div className="text-muted-foreground text-xs">
              {t('assistant.approval.sessionHint')}
            </div>
          </button>
        )}
      </div>
      <div className="mt-2 flex items-center gap-2">
        <Button
          size="sm"
          variant="ghost"
          className="h-7"
          disabled={!!busy}
          onClick={() => send('deny', 'once', DECISION_DENY)}
        >
          {t('approvals.action.deny')}
        </Button>
      </div>
    </div>
  )
}
