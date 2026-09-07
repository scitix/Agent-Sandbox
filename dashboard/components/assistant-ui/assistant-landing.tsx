"use client"

/**
 * The empty state.
 *
 * Its copy is the product explanation, which is why this is ours rather than
 * ported: the suggestions tell a first-time user what this assistant is FOR,
 * and a list describing someone else's domain would teach the wrong thing.
 *
 * The four suggestions are deliberately the onboarding ladder in order —
 * template, environment, pool, SDK — because the most common first question is
 * "where do I even start".
 */

import type { FC } from "react"
import { Boxes, KeyRound, Layers, PlayCircle } from "lucide-react"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { Button } from "@/components/ui/button"

export interface LandingActionSpec {
  labelKey: TranslationKey
  onSelect: () => void
}

export interface AssistantLandingProps {
  /** Sends the text as if the user had typed it. */
  onSuggest?: (prompt: string) => void
  landingActions?: LandingActionSpec[]
}

const SUGGESTIONS: { icon: typeof Layers; key: TranslationKey }[] = [
  { icon: Layers, key: "assistant.suggest.templates" },
  { icon: Boxes, key: "assistant.suggest.createEnv" },
  { icon: KeyRound, key: "assistant.suggest.quota" },
  { icon: PlayCircle, key: "assistant.suggest.runCode" },
]

export const AssistantHero: FC = () => {
  const { t } = useTranslation()
  return (
    <div className="space-y-2 text-center">
      <h1 className="text-2xl font-semibold tracking-tight">
        {t("assistant.hero.title")}
      </h1>
      <p className="text-muted-foreground mx-auto max-w-lg text-sm">
        {t("assistant.hero.subtitle")}
      </p>
    </div>
  )
}

export const AssistantLanding: FC<AssistantLandingProps> = ({
  onSuggest,
  landingActions,
}) => {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-8 p-6">
      <AssistantHero />
      <div className="grid w-full max-w-2xl gap-2 sm:grid-cols-2">
        {SUGGESTIONS.map(({ icon: Icon, key }) => {
          const prompt = t(key)
          return (
            <button
              key={key}
              type="button"
              onClick={() => onSuggest?.(prompt)}
              className="hover:bg-accent hover:text-accent-foreground flex items-start gap-3 rounded-lg border p-3 text-left text-sm transition-colors"
            >
              <Icon className="text-muted-foreground mt-0.5 size-4 shrink-0" />
              <span>{prompt}</span>
            </button>
          )
        })}
      </div>
      {landingActions?.length ? (
        <div className="flex flex-wrap justify-center gap-2">
          {landingActions.map(a => (
            <Button
              key={a.labelKey}
              variant="outline"
              size="sm"
              onClick={a.onSelect}
            >
              {t(a.labelKey)}
            </Button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
