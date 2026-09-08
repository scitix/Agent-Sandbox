import {
  useAgUiInterrupts,
  useAgUiSubmitInterruptResponses,
} from '@assistant-ui/react-ag-ui'
import type { AgUiInterrupt, AgUiResumeEntry } from '@assistant-ui/react-ag-ui'
import { Button } from '@/components/ui/button'
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { useCallback, useMemo, useState } from 'react'

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
}

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
      {cards.map(card => (
        <QuestionCard
          key={card.id}
          request={card.request}
          onReply={answers => flush({ ...answered, [card.id]: answers })}
          onReject={() => flush({ ...answered, [card.id]: [] })}
        />
      ))}
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
  return { id: interrupt.id, request: { questions }, keys }
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
