import {
  type ModelOption,
  useAvailableModels,
} from '@/components/assistant-ui/use-available-models'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useTranslation } from '@/lib/i18n'
import {
  type AssistantModelRef,
  atomAssistantModel,
} from '@/lib/assistant/store'
import { useAtom } from 'jotai'
import { type FC, useEffect } from 'react'

// Encode a model ref as the Select's string value, matching opencode's own
// "providerID/modelID" format. providerID is a simple slug (e.g. "scitix") with
// no "/", so even a modelID containing "/" (e.g. "deepseek-ai/DeepSeek-V4-Flash")
// yields a unique key per option.
const keyOf = (ref: AssistantModelRef) => `${ref.providerID}/${ref.modelID}`
const refOf = (option: ModelOption): AssistantModelRef => ({
  providerID: option.providerID,
  modelID: option.modelID,
})

/** Model picker for the composer action bar. Lists the models opencode actually
 *  has configured (via {@link useAvailableModels}) and writes the choice to
 *  {@link atomAssistantModel}, which the runtime reads live as its
 *  `defaultModel`. Renders nothing unless there's a real choice to make. */
export const ModelSelect: FC = () => {
  const { t } = useTranslation()
  const { options, serverDefault } = useAvailableModels()
  const [selected, setSelected] = useAtom(atomAssistantModel)

  // The default pick when the user hasn't chosen (or their pick vanished from
  // the config): opencode's own default if it's available, else the first model.
  const resolvedDefault =
    (serverDefault && options.find(o => keyOf(o) === keyOf(serverDefault))) ??
    options[0]

  const selectedIsValid =
    selected != null && options.some(o => keyOf(o) === keyOf(selected))

  // Seed / repair the persisted selection once the model list is known.
  useEffect(() => {
    if (options.length === 0) return
    if (!selectedIsValid && resolvedDefault) {
      setSelected(refOf(resolvedDefault))
    }
  }, [options.length, selectedIsValid, resolvedDefault, setSelected])

  // Nothing meaningful to pick — keep the composer clean.
  if (options.length <= 1) return null

  const effective = selectedIsValid ? selected : resolvedDefault
  const value = effective ? keyOf(effective) : undefined

  const handleChange = (next: string | null) => {
    if (!next) return
    const option = options.find(o => keyOf(o) === next)
    if (option) setSelected(refOf(option))
  }

  return (
    <Select value={value} onValueChange={handleChange}>
      <SelectTrigger
        size="sm"
        aria-label={t('assistant.model.select')}
        className="text-muted-foreground hover:text-foreground h-8 min-w-0 gap-1.5 rounded-full border-0 shadow-none dark:bg-transparent dark:hover:bg-transparent"
      >
        {/* The trigger renders the VALUE by default, and the value is the opaque
            `provider/model` key — so it showed "gateway/scitix/DeepSeek-V4-Flash"
            while the dropdown below showed a readable name. Map it back to the
            option's label; fall back to the key so an unknown value is still
            visible rather than blank.

            block! beats the trigger's `*:...:flex` on the value so `truncate` can
            ellipsize a long name in a narrow sidebar. */}
        <SelectValue
          className="block! min-w-0 truncate"
          placeholder={t('assistant.model.select')}
        >
          {/* A children function overrides `placeholder`, so restate it for the
              no-selection case rather than rendering an empty trigger. */}
          {(key: string | null) =>
            (key && options.find(o => keyOf(o) === key)?.label) ??
            key ??
            t('assistant.model.select')
          }
        </SelectValue>
      </SelectTrigger>
      <SelectContent side="top" align="start">
        {options.map(option => (
          <SelectItem key={keyOf(option)} value={keyOf(option)}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
