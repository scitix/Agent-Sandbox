// The assistant's settings, shown in place of the conversation.
//
// Every switch here is backed by the SAME atom the composer's Tools menu writes
// (`atomAssistantAutoOpenPage`, `atomAssistantTopicNudgeEnabled`), so the two
// surfaces are one setting seen twice rather than two settings that agree until
// they don't. The composer keeps its menu because the side panel has no room for
// a settings view and still needs the toggles.
//
// Structured as named sections from the start: "tool management" is the first of
// them, not the whole page, and a second one (models, notifications) must be able
// to land without re-laying-out this file.
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  type TranslationKey,
  useTranslation,
} from '@/lib/i18n'
import {
  atomAssistantAutoOpenPage,
  atomAssistantTopicNudgeEnabled,
} from '@/lib/assistant/store'
import { type PrimitiveAtom, useAtom } from 'jotai'
import {
  ArrowLeftIcon,
  CompassIcon,
  type LucideIcon,
  SplitIcon,
} from 'lucide-react'

interface ToggleSpec {
  key: string
  icon: LucideIcon
  nameKey: TranslationKey
  descriptionKey: TranslationKey
  atom: PrimitiveAtom<boolean>
}

/** What the agent is allowed to do to the UI around it. Both are behaviours the
 *  user might reasonably not want, which is why they are switches and not
 *  product decisions. */
const TOOL_TOGGLES: ToggleSpec[] = [
  {
    key: 'autoOpenPage',
    icon: CompassIcon,
    nameKey: 'assistant.autoOpenPage',
    descriptionKey: 'assistant.settings.autoOpenPageDescription',
    atom: atomAssistantAutoOpenPage,
  },
  {
    key: 'topicNudge',
    icon: SplitIcon,
    nameKey: 'assistant.topicNudge',
    descriptionKey: 'assistant.settings.topicNudgeDescription',
    atom: atomAssistantTopicNudgeEnabled,
  },
]

export function AssistantSettings({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-6 py-8">
      <header className="flex flex-col gap-1">
        {/* The way back lives here, not only in the menu: the menu can be
            collapsed, and settings must never be a room with no door. */}
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground -ms-2 h-7 w-fit gap-1.5 px-2"
          onClick={onClose}
        >
          <ArrowLeftIcon className="size-3.5" />
          {t('assistant.backToChat')}
        </Button>
        <h1 className="text-lg font-medium">{t('assistant.settings.title')}</h1>
        <p className="text-muted-foreground text-sm">
          {t('assistant.settings.subtitle')}
        </p>
      </header>

      <section className="flex flex-col gap-3">
        <div className="flex flex-col gap-0.5">
          <h2 className="text-[13px] font-medium">
            {t('assistant.settings.tools')}
          </h2>
          <p className="text-muted-foreground text-xs">
            {t('assistant.settings.toolsDescription')}
          </p>
        </div>
        <div className="bg-card divide-border divide-y rounded-xl border">
          {TOOL_TOGGLES.map(spec => (
            <ToggleRow key={spec.key} spec={spec} />
          ))}
        </div>
      </section>
    </div>
  )
}

function ToggleRow({ spec }: { spec: ToggleSpec }) {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useAtom(spec.atom)
  const Icon = spec.icon
  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      <span className="bg-muted text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-lg">
        <Icon className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[13px] font-medium">{t(spec.nameKey)}</div>
        <div className="text-muted-foreground truncate text-xs">
          {t(spec.descriptionKey)}
        </div>
      </div>
      <Switch
        checked={enabled}
        onCheckedChange={setEnabled}
        aria-label={t(spec.nameKey)}
      />
    </div>
  )
}
