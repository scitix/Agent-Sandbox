// Topic-switch classifier: does this message start a NEW topic, or continue?
//
// Ported from the workspace-fs daemon so it no longer depends on a harness's
// config file (see model/one-shot.ts for why that mattered). The behaviour is
// deliberately unchanged:
//
//   * the system prompt is VERBATIM — it is a tuned product asset, not code;
//   * the page marker is stripped from BOTH sides, because walking from a list to
//     a detail page is not a change of subject and leaving the markers in makes
//     both axes look like they moved when only the browser did;
//   * the verdict is DERIVED from the two axes rather than read from the model's
//     own `isNewTopic`, which models contradict;
//   * everything fails SAFE to "continuation": this feature must never block a
//     send or nag spuriously, so an error is a non-verdict, not an exception.
import type { OneShotModel } from './model/one-shot.ts'

export const CLASSIFIER_SYSTEM_PROMPT = `You detect topic boundaries in a Kubernetes scheduling assistant, so the UI can offer to move a genuinely new question into its own session.

You are given the conversation exactly as the user sees it: their messages, the assistant's replies, and the tool calls it issued (names and arguments). Judge the new message on two axes.

OBJECT -- the concrete thing under investigation: a pod / workload / node / cluster / team / quota / reservation. It CARRIES OVER when the new message:
- refers to it by name, by pronoun, or by ellipsis. Users write in subject-dropping languages, so a bare "why?", "and the fix?", "from another angle", or "what about the other one" is a follow-up on the current object, not a new one
- asks the next step on it: root cause, fix, YAML, logs, an export, a re-run
- generalises the same question without naming a different object ("other clusters", "any others like it")
- asks about something that first appeared IN THE ANSWER or in a tool call: the node it landed on, its podgroup, its cluster, its quota

GOAL -- the line of enquiry: why is this unschedulable, what is wrong with these nodes, how much quota is left. Read the ANSWER, not only the questions: any cause, constraint, or next step the assistant already surfaced belongs to the current goal. If the diagnosis said the workload was blocked by a quota, then asking about that quota CONTINUES the goal. If it named a taint, asking about that taint continues the goal. Rephrasing, trying another hypothesis for the same symptom, or pointing the SAME enquiry at a different object all keep the goal.

A new topic requires BOTH axes to change: a different object AND a different line of enquiry. If either carries over, it is a continuation.

Sharing a domain is not sharing an axis: every message here is about Kubernetes scheduling, so that tells you nothing.

Reply with ONLY this JSON, no prose, no code fences:
{"objectCarriesOver":boolean,"goalCarriesOver":boolean,"isNewTopic":boolean,"confidence":0..1,"rationale":"at most 14 words"}`

export interface TopicVerdict {
  enabled: boolean
  objectCarriesOver?: boolean
  goalCarriesOver?: boolean
  isNewTopic?: boolean
  confidence?: number
  rationale?: string
  traceId?: string
}

const FAIL_SAFE = {
  objectCarriesOver: true,
  goalCarriesOver: true,
  isNewTopic: false,
  confidence: 0,
  rationale: '',
}

/** How much conversation a check may see. Generous on purpose: truncating drops
 *  the subject and makes a follow-up read as a fresh question. It bounds a
 *  pathological session rather than saving tokens. */
const MAX_CONTEXT_CHARS = Number(
  process.env.ASSISTANT_CLASSIFIER_MAX_CONTEXT || 200_000
)
const MAX_TOKENS = Number(process.env.ASSISTANT_CLASSIFIER_MAX_TOKENS || 512)
const TIMEOUT_MS =
  Number(process.env.ASSISTANT_CLASSIFIER_TIMEOUT_SECONDS || 20) * 1000

const PAGE_MARKER = /<page\s+[^>]*\/>/g

export function stripPageMarkers(text: string): string {
  return (text || '')
    .replace(PAGE_MARKER, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

export function classifierUserMessage(
  context: string,
  newInput: string
): string {
  return (
    `Conversation so far:\n${stripPageMarkers(context)}` +
    `\n\nNew message:\n${stripPageMarkers(newInput)}`
  )
}

/**
 * Pull the two axes out of the reply defensively (tolerating code fences and
 * surrounding prose by slicing the outermost JSON object).
 *
 * The verdict is derived here, not read: a new topic needs BOTH axes to change,
 * and models do contradict themselves on that conjunction — reporting that the
 * object carries over while still calling it a new topic. Only when neither axis
 * is present do we fall back to the model's own `isNewTopic`.
 */
export function parseVerdict(content: string): Omit<TopicVerdict, 'enabled'> {
  let text = (content || '').trim()
  const start = text.indexOf('{')
  const end = text.lastIndexOf('}')
  if (start !== -1 && end > start) text = text.slice(start, end + 1)
  let obj: Record<string, unknown>
  try {
    obj = JSON.parse(text) as Record<string, unknown>
  } catch {
    return { ...FAIL_SAFE }
  }
  const rawConf = Number(obj.confidence)
  const confidence = Number.isFinite(rawConf)
    ? Math.max(0, Math.min(1, rawConf))
    : 0
  const hasAxes = 'objectCarriesOver' in obj || 'goalCarriesOver' in obj
  const objectCarriesOver = obj.objectCarriesOver !== false
  const goalCarriesOver = obj.goalCarriesOver !== false
  const isNewTopic = hasAxes
    ? !objectCarriesOver && !goalCarriesOver
    : Boolean(obj.isNewTopic)
  const rationale =
    typeof obj.rationale === 'string' ? obj.rationale : undefined
  return {
    objectCarriesOver,
    goalCarriesOver,
    isNewTopic,
    confidence,
    rationale,
  }
}

export interface ClassifyRequest {
  context?: string
  newInput: string
  /** Observability only: which conversation and user this check belongs to. */
  threadId?: string | null
  userKey?: string
}

export interface Classifier {
  classify(req: ClassifyRequest): Promise<TopicVerdict>
}

export function createClassifier(
  model: OneShotModel | null,
  report?: (record: ClassifierRecord) => void
): Classifier {
  return {
    async classify(req): Promise<TopicVerdict> {
      // `enabled:false` tells the frontend the feature is off, so it does not
      // even show the affordance — distinct from "on, but this is a continuation".
      if (!model) return { enabled: false }
      const context = (req.context || '').slice(0, MAX_CONTEXT_CHARS)
      const user = classifierUserMessage(context, req.newInput)
      const result = await model.complete({
        system: CLASSIFIER_SYSTEM_PROMPT,
        user,
        maxTokens: MAX_TOKENS,
        timeoutMs: TIMEOUT_MS,
      })
      const verdict = result.error
        ? { ...FAIL_SAFE }
        : parseVerdict(result.content)
      report?.({ req, result, verdict, user })
      return { enabled: true, ...verdict }
    },
  }
}

export interface ClassifierRecord {
  req: ClassifyRequest
  result: Awaited<ReturnType<OneShotModel['complete']>>
  verdict: Omit<TopicVerdict, 'enabled'>
  /** The exact user message sent, for the trace's input. */
  user: string
}
