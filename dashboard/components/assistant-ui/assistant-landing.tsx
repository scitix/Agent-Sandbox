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
 * What the assistant shows before the first message: who you are, what it can
 * do, what you might ask, and what your environments are reporting right now.
 *
 * Everything here PRE-FILLS the composer and sends nothing. The landing is a way
 * into a conversation, not a set of buttons that start one behind your back.
 *
 * Two phrase banks, deliberately distinct in voice:
 *   * capabilities ("I can …") rotate under the greeting — the assistant saying
 *     what it is for, in its own voice;
 *   * suggestions ("Which templates can I use?") are phrased as the USER would
 *     ask, because clicking one puts those exact words in their composer.
 */

import { type FC, useEffect, useState } from "react"
import { useAtomValue } from "jotai"
import { type LucideIcon, RefreshCwIcon } from "lucide-react"
import { useAui } from "@assistant-ui/react"

import {
  StatusCards,
  useLandingSignals,
  type StatusAsk,
} from "@/components/assistant-ui/status-cards"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { actingIdentityAtom } from "@/lib/atoms"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** How long each capability line holds before the next flips in. */
const CAPABILITY_ROTATE_MS = 4200

/** How many suggestions are on screen at once. They lay out as a wrapping row,
 *  so this is about how much of the bank you see rather than a grid size;
 *  "shuffle" advances the window rather than re-randomising, so the same click
 *  never shows the same batch twice in a row. */
const SUGGESTION_WINDOW = 6

/**
 * A note on what these say.
 *
 * The label is the question a person clicks and the prompt is what reaches the
 * agent — but neither carries operating instructions. "Ask me my target
 * concurrency before configuring anything" is true of every rollout
 * conversation, not of the one that happened to start from this pill, so it
 * belongs in the skill that handles rollouts. Put in the prompt, it reads as
 * something the user typed, which makes it advice the agent may drop the moment
 * the user says anything else.
 */

/** How long the skeleton holds after a shuffle. */
const SHUFFLE_PAUSE_MS = 1000

/** The assistant's own voice. Ordered from the most common question inwards. */
const CAPABILITY_KEYS: TranslationKey[] = [
  "assistant.capability.runCode",
  "assistant.capability.envs",
  "assistant.capability.pools",
  "assistant.capability.stuckSandbox",
  "assistant.capability.quota",
  "assistant.capability.templates",
  "assistant.capability.files",
  "assistant.capability.images",
]

/**
 * What the pill SAYS and what it ASKS are two different strings.
 *
 * The pill has to be readable at a glance, so it is one short question. The
 * prompt behind it is what actually reaches the agent, and it can afford to be
 * specific — naming the `abx` command to start from, the grouping to report, the
 * follow-up worth doing. Collapsing the two would force a choice between a pill
 * nobody can scan and a question the agent has to guess at.
 *
 * Keys are listed as literal pairs rather than derived from an id, so a missing
 * or misspelled one is a type error instead of a runtime blank.
 */
interface SuggestionSpec {
  id: string
  labelKey: TranslationKey
  promptKey: TranslationKey
  /**
   * When this question is worth asking, given what the deployment looks like.
   *
   * Absent means always. A suggestion that presumes an environment exists is
   * noise on a fresh install, and "why won't my pool scale" is noise until a
   * pool is actually stuck — the bank is small enough that a slot spent on an
   * irrelevant question is a slot not spent on the useful one.
   */
  when?: (s: LandingSignals) => boolean
}

/**
 * The few facts that change which questions are worth offering.
 *
 * Derived from the queries the status board already runs, so this costs no
 * extra request — the cache is shared and the landing renders one set of data.
 */
export interface LandingSignals {
  hasEnv: boolean
  hasStuckPool: boolean
  hasFailedSandbox: boolean
}



const SUGGESTIONS: SuggestionSpec[] = [
  // What people actually come here to do. The two largest real uses of the
  // platform — running rollouts at scale, and putting sandboxes behind your own
  // agent — had no suggestion at all, while four of ten asked the assistant to
  // list something the user can see by clicking a page.
  {
    id: "rlRollouts",
    labelKey: "assistant.ask.rlRollouts.label",
    promptKey: "assistant.ask.rlRollouts.prompt",
  },
  {
    id: "handsIntegration",
    labelKey: "assistant.ask.handsIntegration.label",
    promptKey: "assistant.ask.handsIntegration.prompt",
  },
  {
    id: "harborEval",
    labelKey: "assistant.ask.harborEval.label",
    promptKey: "assistant.ask.harborEval.prompt",
    when: s => s.hasEnv,
  },
  {
    id: "concurrency",
    labelKey: "assistant.ask.concurrency.label",
    promptKey: "assistant.ask.concurrency.prompt",
    when: s => s.hasEnv,
  },
  {
    id: "createEnv",
    labelKey: "assistant.ask.createEnv.label",
    promptKey: "assistant.ask.createEnv.prompt",
    when: s => !s.hasEnv,
  },
  {
    id: "poolNotScaling",
    labelKey: "assistant.ask.poolNotScaling.label",
    promptKey: "assistant.ask.poolNotScaling.prompt",
    when: s => s.hasStuckPool,
  },
  {
    id: "stuckSandbox",
    labelKey: "assistant.ask.stuckSandbox.label",
    promptKey: "assistant.ask.stuckSandbox.prompt",
    when: s => s.hasFailedSandbox,
  },
  {
    id: "runCode",
    labelKey: "assistant.ask.runCode.label",
    promptKey: "assistant.ask.runCode.prompt",
    when: s => s.hasEnv,
  },
  {
    id: "installDeps",
    labelKey: "assistant.ask.installDeps.label",
    promptKey: "assistant.ask.installDeps.prompt",
    when: s => s.hasEnv,
  },
  {
    id: "autoscaling",
    labelKey: "assistant.ask.autoscaling.label",
    promptKey: "assistant.ask.autoscaling.prompt",
    when: s => s.hasEnv,
  },
  {
    id: "uploadFiles",
    labelKey: "assistant.ask.uploadFiles.label",
    promptKey: "assistant.ask.uploadFiles.prompt",
    when: s => s.hasEnv,
  },
  {
    // Not gated: "what can this platform run" is the first question on a fresh
    // install and still a fair one later, when someone wants a different shape
    // of workload than the env they already have.
    id: "templates",
    labelKey: "assistant.ask.templates.label",
    promptKey: "assistant.ask.templates.prompt",
  },
  {
    id: "docker",
    labelKey: "assistant.ask.docker.label",
    promptKey: "assistant.ask.docker.prompt",
  },
  {
    id: "noEgress",
    labelKey: "assistant.ask.noEgress.label",
    promptKey: "assistant.ask.noEgress.prompt",
  },
  {
    id: "vault",
    labelKey: "assistant.ask.vault.label",
    promptKey: "assistant.ask.vault.prompt",
  },
]

/** How many static suggestions sit between two injected actions. Interleaved
 *  rather than appended, so a build whose actions are the interesting part does
 *  not hide them behind two shuffles. */
const ACTION_EVERY = 3

/**
 * A suggestion that carries something extra — a "connect your CLI" walkthrough,
 * say, which prefills a question the assistant can answer from a document it
 * already has.
 *
 * It joins the same rotating bank as the static phrases rather than sitting in a
 * row of its own: to the user both are "something you could ask", and giving the
 * ones with an attachment their own chip style would say they are a different
 * kind of action when they are not.
 */
export interface LandingActionSpec {
  key: string
  icon: LucideIcon
  labelKey: TranslationKey
  /** Inserted into the composer, unsent, exactly like every other suggestion. */
  prompt: () => string
}

export interface AssistantLandingProps {
  /** Extra suggestions, mixed into the bank. */
  actions?: LandingActionSpec[]
  /** Pre-fill the composer. Suggestions never send — the user does. */
  onPick: (prompt: string) => void
  /** A status card: go to the page that number lives on, and ask about it. */
  onAsk: (ask: StatusAsk) => void
}

/** Greeting + the rotating "I can …" line. Shown on every empty conversation,
 *  including the narrow side panel where the rest of the landing is not. */
export const AssistantHero: FC = () => {
  const { t } = useTranslation()
  const { user } = useAtomValue(actingIdentityAtom)

  return (
    <div className="flex flex-col items-center gap-2.5 text-center">
      {/* Sized against the THREAD's container, not the viewport: the same
          greeting has a page to spread across and a 38rem-wide side panel to
          fit in. */}
      <h1 className="text-xl font-medium leading-tight @lg:text-2xl">
        {user
          ? t("assistant.greeting.named", { name: user })
          : t("assistant.greeting.anonymous")}
      </h1>
      <CapabilityFlip />
    </div>
  )
}

/**
 * The "I can …" line, one phrase at a time.
 *
 * Every phrase is rendered stacked in a single grid cell rather than as rows of
 * a strip that gets translated: a translated strip has to be moved by exactly
 * one row height, and the only unit that is cheap to write — a percentage —
 * resolves against the STRIP, not a row, so it drifts the moment the strip is
 * more than one row tall. Stacking also makes the box as tall as the tallest
 * phrase, so a line that wraps to two rows in the narrow side panel is shown
 * whole instead of clipped, and the height never jumps mid-rotation.
 *
 * Each phrase then only has to know one of three states — arriving from below,
 * holding, leaving upwards — which is what gives the flip its direction.
 */
const CapabilityFlip: FC = () => {
  const { t } = useTranslation()
  // `leaving` is the index the rotation just moved off. It is kept next to the
  // current one (rather than in a ref) because the outgoing phrase has to be
  // rendered in its exit state in the very same commit that promotes the new
  // one — a ref would update a frame late and the old line would vanish.
  const [{ current, leaving }, setSlide] = useState({ current: 0, leaving: -1 })

  useEffect(() => {
    const id = window.setInterval(
      () =>
        setSlide(s => ({
          current: (s.current + 1) % CAPABILITY_KEYS.length,
          leaving: s.current,
        })),
      CAPABILITY_ROTATE_MS
    )
    return () => window.clearInterval(id)
  }, [])

  return (
    <p className="text-muted-foreground grid overflow-hidden text-sm [perspective:600px]">
      {CAPABILITY_KEYS.map((key, i) => (
        <span
          key={key}
          // Same cell for all of them: the stack, and the reason the box is
          // sized to the tallest phrase.
          className={cn(
            "text-muted-foreground/80 col-start-1 row-start-1 self-center leading-5",
            "transition-[transform,opacity,filter] duration-500 ease-out motion-reduce:transition-none",
            i === current
              ? "opacity-100 blur-0 [transform:none]"
              : "opacity-0 blur-[2px]",
            i !== current &&
              (i === leaving
                ? "[transform:translateY(-70%)_rotateX(35deg)]"
                : "[transform:translateY(70%)_rotateX(-35deg)]")
          )}
          // Only the phrase on screen is read out; the rest are a rendering
          // detail, and a line that re-announces itself every few seconds is
          // noise to a screen reader.
          aria-hidden={i !== current}
        >
          {t(key)}
        </span>
      ))}
    </p>
  )
}

/** The scrolling part of the landing: suggestions and the status board.
 *  Rendered below the composer, so the eye lands on the input first and the
 *  ideas are what it falls to next. */
export const AssistantLanding: FC<AssistantLandingProps> = ({
  actions,
  onPick,
  onAsk,
}) => {
  const signals = useLandingSignals()
  return (
    <div className="flex w-full flex-col gap-10 pb-16">
      <SuggestedQuestions actions={actions} onPick={onPick} signals={signals} />
      <StatusCards onAsk={onAsk} />
    </div>
  )
}

/** One thing you could ask, whatever produced it. */
interface Suggestion {
  key: string
  label: string
  icon?: LucideIcon
  run: () => void
}

function SuggestedQuestions({
  actions,
  onPick,
  signals,
}: {
  actions?: LandingActionSpec[]
  onPick: (prompt: string) => void
  signals: LandingSignals
}) {
  const { t } = useTranslation()
  const [offset, setOffset] = useState(0)
  // A beat of skeleton on shuffle. Swapping six labels instantly reads as a
  // glitch — the row flickers and you cannot tell whether anything changed;
  // holding the shape for a moment makes it legible as "these are new".
  const [shuffling, setShuffling] = useState(false)
  useEffect(() => {
    if (!shuffling) return
    const id = window.setTimeout(() => setShuffling(false), SHUFFLE_PAUSE_MS)
    return () => window.clearTimeout(id)
  }, [shuffling])

  const statics: Suggestion[] = SUGGESTIONS.filter(
    spec => !spec.when || spec.when(signals)
  ).map(spec => ({
    key: spec.id,
    label: t(spec.labelKey),
    // The pill's words go nowhere; the prompt behind it is what is asked.
    run: () => onPick(t(spec.promptKey)),
  }))
  const injected: Suggestion[] = (actions ?? []).map(action => ({
    key: action.key,
    label: t(action.labelKey),
    icon: action.icon,
    run: () => onPick(action.prompt()),
  }))
  const bank: Suggestion[] = []
  const queue = [...injected]
  statics.forEach((item, i) => {
    bank.push(item)
    const next = (i + 1) % ACTION_EVERY === 0 ? queue.shift() : undefined
    if (next) bank.push(next)
  })
  bank.push(...queue)

  // A rotating window, not a random sample: shuffling then shows a batch the
  // user has not just dismissed, and the set is stable across re-renders.
  const shown = Array.from(
    { length: Math.min(SUGGESTION_WINDOW, bank.length) },
    (_, i) => bank[(offset + i) % bank.length]
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h2 className="text-[13px] font-medium">{t("assistant.ask.title")}</h2>
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground/70 hover:text-muted-foreground -me-2 h-7 gap-1.5 rounded-full px-2.5 text-xs"
          disabled={shuffling}
          onClick={() => {
            setShuffling(true)
            setOffset(o => o + SUGGESTION_WINDOW)
          }}
        >
          <RefreshCwIcon
            className={cn("size-3.5", shuffling && "animate-spin")}
          />
          {t("assistant.ask.shuffle")}
        </Button>
      </div>
      {/* A wrapping row of pills, sized to their text — a grid would stretch
          "What templates can I use?" to the width of the longest question and
          leave the short ones mostly empty. */}
      <div className="flex flex-wrap gap-2">
        {shuffling
          ? shown.map((item, i) => (
              <Skeleton
                key={item.key}
                className="h-9 rounded-full"
                // Varied widths, so the placeholder row has the silhouette of
                // questions rather than of six identical bars.
                style={{ width: `${[13, 17, 11, 15, 9, 14][i % 6]}rem` }}
              />
            ))
          : shown.map(item => {
              const Icon = item.icon
              return (
                <button
                  key={item.key}
                  type="button"
                  onClick={item.run}
                  className="bg-card hover:border-primary/40 hover:text-foreground text-muted-foreground flex h-9 items-center gap-1.5 rounded-full border px-3.5 text-left text-[13px] transition-colors"
                >
                  {Icon && <Icon className="size-3.5 shrink-0" />}
                  {item.label}
                </button>
              )
            })}
      </div>
    </section>
  )
}

/** Fills the composer with a suggestion's words, without sending them. */
export function useComposerPrefill(): (prompt: string) => void {
  const aui = useAui()
  return (prompt: string) => aui.composer().setText(prompt)
}
