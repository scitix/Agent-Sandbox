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

import { Fragment, useEffect, useMemo, useState } from "react"
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { useDebouncedCallback } from "use-debounce"
import { zodResolver } from "@hookform/resolvers/zod"
import { toast } from "sonner"
import { Loader2, Plus, Save, Trash2 } from "lucide-react"

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import { Button } from "@/components/ui/button"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { CopyEnvDialog } from "@/components/envs/copy-env-dialog"
import { FormCloneActions } from "@/components/custom/form-clone-actions"
import type { AgentSandboxEnv, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { useEnvNameAcrossClusters } from "@/hooks/use-env-name-across-clusters"
import { envQueryOptions, templatesQueryOptions, useCreateEnv, useUpdateEnv } from "@/lib/queries"
import { envClone } from "@/lib/utils/env-clone"
import {
  envFormDefaults,
  envToFormValues,
  formSchema,
  formValuesToCreateBody,
  formValuesToUpdateBody,
  type FormValues,
} from "@/lib/utils/env-form"
import { useTranslation } from "@/lib/i18n"

interface Props {
  /**
   * Name of the env to edit. Omitted/null opens the sheet in create mode.
   * The full SandboxEnv is refetched via GET so the form never relies on a
   * possibly-stale list-row projection.
   */
  envName?: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function UpsertEnvSheet({ envName, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        {open && <UpsertEnvLoader envName={envName ?? null} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

// Resolves edit mode to a full SandboxEnv (refetched via GET) before mounting
// the form, so the form's defaultValues are built from authoritative state
// rather than the trimmed list-row summary. Create mode mounts immediately.
function UpsertEnvLoader({ envName, onClose }: { envName: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    ...envQueryOptions(envName ?? ""),
    enabled: !!envName,
  })

  if (envName && isLoading) {
    return (
      <>
        <SheetHeader className="border-b px-6 py-4">
          <SheetTitle>{t("envs.form.editTitle")}</SheetTitle>
        </SheetHeader>
        <div className="text-muted-foreground flex-1 px-6 py-8 text-sm">{t("common.loading")}</div>
      </>
    )
  }

  return <UpsertEnvForm env={envName ? (data?.env ?? null) : null} onClose={onClose} />
}

interface InnerProps {
  env: AgentSandboxEnv | null
  onClose: () => void
}

function UpsertEnvForm({ env, onClose }: InnerProps) {
  const { t } = useTranslation()
  const isEdit = !!env

  const { data: templates = [] } = useQuery(templatesQueryOptions())

  const defaultValues = useMemo<FormValues>(() => envToFormValues(env), [env])

  const {
    control,
    register,
    handleSubmit,
    reset,
    trigger,
    getValues,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues,
  })

  const createMutation = useCreateEnv()
  const updateMutation = useUpdateEnv()

  // ── Name availability (create only) ────────────────────────────────────────
  //
  // An Env name is meaningful beyond this cluster: the same name on another
  // cluster with the same template is the same logical environment, extended.
  // So a name is checked against every cluster, not just this one — taken here
  // is an error, taken elsewhere is an offer to copy that env's configuration.
  //
  // The probe runs off the per-cluster Env lists, which are fetched once and
  // then cached; debouncing gates when the typed name is *compared*, so no
  // request is made per keystroke.
  const typedName = useWatch({ control, name: "name" }) ?? ""
  const [checkedName, setCheckedName] = useState("")
  const settleName = useDebouncedCallback((v: string) => setCheckedName(v), 400)

  useEffect(() => {
    if (isEdit) return
    settleName(typedName.trim())
  }, [typedName, isEdit, settleName])

  const presence = useEnvNameAcrossClusters(isEdit ? "" : checkedName)
  const nameSettled = !isEdit && checkedName === typedName.trim()
  const isCheckingName = !isEdit && typedName.trim() !== "" && (!nameSettled || presence.isProbing)

  const takenHere = nameSettled && presence.current?.state === "present"
  const takenElsewhere = useMemo(
    () => (nameSettled ? presence.others.filter((p) => p.state === "present") : []),
    [nameSettled, presence.others],
  )

  // Whether the copy offer is showing is derived, not stored: the only thing
  // worth remembering is which name the user has already said no to, so
  // dismissing it does not re-prompt on the next render for that same name.
  const [copyDismissed, setCopyDismissed] = useState<string | null>(null)
  const copyOfferFor =
    !takenHere && takenElsewhere.length > 0 && copyDismissed !== checkedName ? checkedName : null

  const onSubmit = handleSubmit(async (values) => {
    if (isEdit) {
      const body = formValuesToUpdateBody(values)
      await new Promise<void>((resolve, reject) => {
        updateMutation.mutate(
          { params: { path: { name: env!.name } }, body },
          {
            onSuccess: () => {
              toast.success(t("envs.form.toast.updated", { name: env!.name }))
              onClose()
              resolve()
            },
            onError: (err) => reject(err),
          },
        )
      }).catch((err: unknown) => toast.error(extractError(err)))
    } else {
      const body = formValuesToCreateBody(values)
      await new Promise<void>((resolve, reject) => {
        createMutation.mutate(
          { body },
          {
            onSuccess: () => {
              toast.success(t("envs.form.toast.created", { name: values.name }))
              onClose()
              resolve()
            },
            onError: (err) => reject(err),
          },
        )
      }).catch((err: unknown) => toast.error(extractError(err)))
    }
  })

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {isEdit ? t("envs.form.editTitle") : t("envs.form.createTitle")}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
          {/* Basics — always visible */}
          <section className="space-y-4">
            <Field>
              <FieldLabel htmlFor="env-name">{t("envs.form.name")}</FieldLabel>
              <div className="relative">
                <Input
                  id="env-name"
                  disabled={isEdit}
                  aria-invalid={!!errors.name || takenHere}
                  aria-busy={isCheckingName}
                  {...register("name")}
                  placeholder="my-env"
                  maxLength={24}
                  className={isCheckingName ? "pr-9" : undefined}
                />
                {isCheckingName && (
                  <Loader2
                    aria-hidden
                    className="text-muted-foreground pointer-events-none absolute top-1/2 right-3 h-4 w-4 -translate-y-1/2 animate-spin"
                  />
                )}
              </div>
              {errors.name ? (
                <FieldError>{t(errors.name.message as never)}</FieldError>
              ) : takenHere ? (
                <FieldError>{t("envs.form.nameCheck.takenHere")}</FieldError>
              ) : null}
              <FieldDescription>
                {isCheckingName
                  ? t("envs.form.nameCheck.checking")
                  : takenElsewhere.length > 0
                    ? t("envs.form.nameCheck.takenElsewhere", {
                        clusters: takenElsewhere.map((p) => p.clusterName).join(", "),
                      })
                    : t("envs.form.nameDescription")}
              </FieldDescription>
              {takenElsewhere.length > 0 && !takenHere && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="mt-1 h-7 w-fit text-xs"
                  onClick={() => setCopyDismissed(null)}
                >
                  {t("envs.form.copyFrom.reopen")}
                </Button>
              )}
            </Field>
          </section>

          {/* Everything past the name is gated on the availability check: a name
              that turns out to be taken here, or copied from another cluster,
              changes what the rest of the form should say. */}
          <fieldset disabled={isCheckingName} className="min-w-0 space-y-5 disabled:opacity-60">
            <section className="space-y-4">
              <Field>
                <FieldLabel>{t("envs.form.template")}</FieldLabel>
                <Controller
                  control={control}
                  name="templateName"
                  render={({ field, fieldState }) => (
                    <TemplateCombobox
                      items={templates}
                      value={field.value}
                      onChange={field.onChange}
                      invalid={fieldState.invalid}
                      disabled={isEdit}
                    />
                  )}
                />
                {errors.templateName && (
                  <FieldError>{t(errors.templateName.message as never)}</FieldError>
                )}
                <SelectedTemplateInfo templates={templates} control={control} />
              </Field>
            </section>

            {/* Advanced — one collapsed panel per concern, so the headers alone
                say what is configurable without anything being expanded. */}
            <div className="border-border divide-border divide-y rounded-md border">
              <Accordion>
                <AccordionItem value="image">
                  <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                    {t("envs.form.section.imageAndTimeout")}
                  </AccordionTrigger>
                  <AccordionContent className="px-3">
                    <div className="flex flex-col gap-5 pb-2">
                      {/* Env-level overrides */}
                      <section className="space-y-3">
                        <p className="text-muted-foreground text-xs">
                          {t("envs.form.overridesHint")}
                        </p>

                        <Field>
                          <FieldLabel htmlFor="env-image">{t("envs.form.image")}</FieldLabel>
                          <Input
                            id="env-image"
                            {...register("image")}
                            placeholder="ghcr.io/org/runtime:1.2"
                            className="font-mono text-sm"
                          />
                          <FieldDescription>{t("envs.form.imageDescription")}</FieldDescription>
                        </Field>

                        <Field>
                          <FieldLabel>{t("envs.form.podCreationImagePolicy")}</FieldLabel>
                          <Controller
                            control={control}
                            name="podCreationImagePolicy"
                            render={({ field }) => (
                              <Select
                                value={field.value ?? ""}
                                onValueChange={(v) => field.onChange(v || undefined)}
                              >
                                <SelectTrigger>
                                  <SelectValue placeholder={t("envs.form.imagePolicyDefault")} />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="IdleImage">IdleImage</SelectItem>
                                  <SelectItem value="PoolDefaultImage">PoolDefaultImage</SelectItem>
                                </SelectContent>
                              </Select>
                            )}
                          />
                          <FieldDescription>
                            {t("envs.form.podCreationImagePolicyDescription")}
                          </FieldDescription>
                        </Field>

                        <div className="grid grid-cols-2 gap-3">
                          <Field>
                            <FieldLabel htmlFor="env-startup">
                              {t("envs.form.defaultStartupTimeout")}
                            </FieldLabel>
                            <Input
                              id="env-startup"
                              placeholder="5m"
                              {...register("defaultStartupTimeout")}
                            />
                            <FieldDescription>
                              {t("envs.form.defaultStartupTimeoutDescription")}
                            </FieldDescription>
                          </Field>
                          <Field>
                            <FieldLabel htmlFor="env-idle">
                              {t("envs.form.defaultIdleTimeout")}
                            </FieldLabel>
                            <Input
                              id="env-idle"
                              placeholder="30m"
                              {...register("defaultIdleTimeout")}
                            />
                            <FieldDescription>
                              {t("envs.form.defaultIdleTimeoutDescription")}
                            </FieldDescription>
                          </Field>
                        </div>
                      </section>

                      <Separator />

                      <ImagePullSecretSection control={control} register={register} />
                    </div>
                  </AccordionContent>
                </AccordionItem>

                <AccordionItem value="network">
                  <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                    {t("envs.form.section.networkPolicy")}
                  </AccordionTrigger>
                  <AccordionContent className="px-3">
                    <div className="flex flex-col gap-5 pb-2">
                      <NetworkPolicySection control={control} register={register} errors={errors} />
                      <Separator />
                      <SecretInjectionSection
                        control={control}
                        register={register}
                        errors={errors}
                      />
                    </div>
                  </AccordionContent>
                </AccordionItem>

                <AccordionItem value="update">
                  {/* The switch is a SIBLING of the trigger, not a child: the
                      trigger is itself a <button>, so nesting would be invalid
                      markup and every toggle would also open the panel. */}
                  <div className="flex items-center pr-3">
                    <AccordionTrigger className="text-muted-foreground flex-1 px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                      {t("envs.form.section.updateStrategy")}
                    </AccordionTrigger>
                    <Controller
                      control={control}
                      name="autoUpdate"
                      render={({ field }) => (
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label={t("envs.form.updateStrategy.autoUpdate")}
                        />
                      )}
                    />
                  </div>
                  <AccordionContent className="px-3">
                    <UpdateStrategySection control={control} register={register} />
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            </div>
          </fieldset>
        </div>

        <Separator />
        <div className="flex items-center gap-2 px-6 py-3">
          <FormCloneActions
            clone={envClone}
            getValues={getValues}
            defaults={envFormDefaults()}
            canImport={!isEdit}
            onImport={(v) => {
              reset(v)
              void trigger()
            }}
          />
          <div className="ml-auto flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              disabled={isSubmitting || isCheckingName || takenHere}
              className="gap-1.5"
            >
              <Save className="h-3.5 w-3.5" />
              {isEdit ? t("common.save") : t("common.create")}
            </Button>
          </div>
        </div>
      </form>

      {/* Same name on another cluster: offer to start from that env's config. */}
      <CopyEnvDialog
        name={copyOfferFor}
        candidates={takenElsewhere}
        onClose={() => setCopyDismissed(copyOfferFor)}
        onCopy={(source) => {
          reset({ ...envToFormValues(source), name: source.name })
          setCopyDismissed(copyOfferFor)
          toast.success(t("envs.form.copyFrom.applied", { name: source.name }))
        }}
      />
    </Fragment>
  )
}

// ─── Combobox ────────────────────────────────────────────────────────────────

function TemplateCombobox({
  items,
  value,
  onChange,
  invalid,
  disabled,
}: {
  items: AgentSandboxTemplateSummary[]
  value: string | undefined
  onChange: (v: string) => void
  invalid?: boolean
  disabled?: boolean
}) {
  const selected = items.find((it) => it.name === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item.name}
      value={selected}
      onValueChange={(v) => onChange(v?.name ?? "")}
      disabled={disabled}
    >
      <ComboboxInput aria-invalid={invalid} placeholder="select template" />
      <ComboboxContent>
        <ComboboxList>
          {(item: AgentSandboxTemplateSummary) => (
            <ComboboxItem key={item.name} value={item}>
              <div className="flex w-full min-w-0 flex-col gap-0.5">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs">{item.name}</span>
                  {item.version && (
                    <span className="border-border bg-muted text-muted-foreground rounded border px-1 font-mono text-[10px]">
                      {item.version}
                    </span>
                  )}
                </div>
                {item.description && (
                  <span className="text-muted-foreground truncate text-[10px]">
                    {item.description}
                  </span>
                )}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
  )
}

function SelectedTemplateInfo({
  templates,
  control,
}: {
  templates: AgentSandboxTemplateSummary[]
  control: ReturnType<typeof useForm<FormValues>>["control"]
}) {
  const { t } = useTranslation()
  const name = useWatch({ control, name: "templateName" })
  const tpl = templates.find((it) => it.name === name)
  if (!tpl) {
    return <FieldDescription>{t("envs.form.templateDescription")}</FieldDescription>
  }
  return (
    <div className="border-border bg-muted/30 mt-1 space-y-1 rounded border border-dashed px-2 py-1.5">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px]">
        <span className="text-foreground">{tpl.name}</span>
        {tpl.version && (
          <span className="border-border bg-background text-muted-foreground rounded border px-1 text-[10px]">
            {tpl.version}
          </span>
        )}
        {(tpl.cpu || tpl.memory) && (
          <span className="text-muted-foreground text-[10px]">
            {[tpl.cpu && `${tpl.cpu}c`, tpl.memory].filter(Boolean).join(" / ")}
          </span>
        )}
      </div>
      {tpl.description ? (
        <p className="text-muted-foreground text-[11px] leading-snug">{tpl.description}</p>
      ) : (
        <p className="text-muted-foreground text-[11px] italic">
          {t("envs.form.templateNoDescription")}
        </p>
      )}
    </div>
  )
}

// ─── ImagePullSecret section ────────────────────────────────────────────────

interface ImagePullSecretSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
}

function ImagePullSecretSection({ control, register }: ImagePullSecretSectionProps) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({ control, name: "imagePullSecretRows" })
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.imagePullSecret")}
        </h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => append({ registry: "", username: "", password: "" })}
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.imagePullSecret.addRegistry")}
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">{t("envs.form.imagePullSecret.hint")}</p>
      {fields.length === 0 && (
        <p className="text-muted-foreground rounded-md border border-dashed px-3 py-3 text-center text-xs">
          {t("envs.form.imagePullSecret.empty")}
        </p>
      )}
      <div className="space-y-2">
        {fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
          >
            <div className="grid flex-1 grid-cols-3 gap-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.registry")}
                </FieldLabel>
                <Input
                  placeholder="ghcr.io"
                  {...register(`imagePullSecretRows.${index}.registry` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.username")}
                </FieldLabel>
                <Input
                  placeholder="user"
                  autoComplete="off"
                  {...register(`imagePullSecretRows.${index}.username` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.password")}
                </FieldLabel>
                <Input
                  type="password"
                  placeholder="••••••"
                  autoComplete="new-password"
                  {...register(`imagePullSecretRows.${index}.password` as const)}
                />
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => remove(index)}
              className="text-destructive mt-5"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>
    </section>
  )
}

interface SecretInjectionSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
}

function SecretInjectionSection({ control, register, errors }: SecretInjectionSectionProps) {
  const { t } = useTranslation()
  const creds = useFieldArray({ control, name: "injectionCredentialRows" })
  const rules = useFieldArray({ control, name: "injectionRuleRows" })

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.secretInjection")}
        </h3>
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.secretInjection.hint")}</p>
      </div>

      <div className="flex items-center justify-between">
        <FieldLabel className="text-[11px]">
          {t("envs.form.secretInjection.credentials")}
        </FieldLabel>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() =>
            creds.append({ name: "", value: "", configured: false, exposeAs: "", placeholder: "" })
          }
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.secretInjection.addCredential")}
        </Button>
      </div>
      {creds.fields.length === 0 && (
        <p className="text-muted-foreground rounded-md border border-dashed px-3 py-3 text-center text-xs">
          {t("envs.form.secretInjection.empty")}
        </p>
      )}
      <div className="space-y-2">
        {creds.fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
          >
            <div className="flex-1 space-y-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.credName")}
                </FieldLabel>
                <Input
                  placeholder="openai"
                  {...register(`injectionCredentialRows.${index}.name` as const)}
                />
                <FieldDescription className="text-[11px]">
                  {t("envs.form.secretInjection.credNameDescription")}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.value")}
                  {creds.fields[index]?.configured && (
                    <span className="text-muted-foreground ml-2 font-normal">
                      {t("envs.form.secretInjection.valueStored")}
                    </span>
                  )}
                </FieldLabel>
                <Input
                  type="password"
                  autoComplete="new-password"
                  placeholder={
                    creds.fields[index]?.configured
                      ? t("envs.form.secretInjection.valueKeep")
                      : "sk-..."
                  }
                  {...register(`injectionCredentialRows.${index}.value` as const)}
                />
                <FieldDescription className="text-[11px]">
                  {t("envs.form.secretInjection.valueDescription")}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.exposeAs")}
                </FieldLabel>
                <Input
                  placeholder="OPENAI_API_KEY"
                  {...register(`injectionCredentialRows.${index}.exposeAs` as const)}
                />
                <FieldDescription className="text-[11px]">
                  {t("envs.form.secretInjection.exposeAsDescription")}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.placeholder")}
                </FieldLabel>
                <Input
                  placeholder={t("envs.form.secretInjection.placeholderHint")}
                  {...register(`injectionCredentialRows.${index}.placeholder` as const)}
                />
                <FieldDescription className="text-[11px]">
                  {t("envs.form.secretInjection.placeholderDescription")}
                </FieldDescription>
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => creds.remove(index)}
              className="text-destructive mt-5"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>

      <div className="flex items-center justify-between pt-1">
        <FieldLabel className="text-[11px]">{t("envs.form.secretInjection.rules")}</FieldLabel>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() =>
            rules.append({
              host: "",
              headerName: "Authorization",
              headerValue: "",
              mode: "Override",
              substitute: "",
              pathPrefixes: "",
            })
          }
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.secretInjection.addRule")}
        </Button>
      </div>
      <div className="space-y-2">
        {rules.fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
          >
            <div className="flex-1 space-y-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.host")}
                </FieldLabel>
                <Input
                  placeholder="api.openai.com"
                  {...register(`injectionRuleRows.${index}.host` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.headerName")}
                </FieldLabel>
                <Input
                  placeholder="Authorization"
                  {...register(`injectionRuleRows.${index}.headerName` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.headerValue")}
                </FieldLabel>
                <Input
                  placeholder="Bearer {{ openai }}"
                  {...register(`injectionRuleRows.${index}.headerValue` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.mode")}
                </FieldLabel>
                <Controller
                  control={control}
                  name={`injectionRuleRows.${index}.mode` as const}
                  render={({ field: f }) => (
                    <Select value={f.value ?? "Override"} onValueChange={f.onChange}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="Override">
                          {t("envs.form.secretInjection.modeOverride")}
                        </SelectItem>
                        <SelectItem value="IfAbsent">
                          {t("envs.form.secretInjection.modeIfAbsent")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.substitute")}
                </FieldLabel>
                <Input
                  placeholder="openai"
                  {...register(`injectionRuleRows.${index}.substitute` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.pathPrefixes")}
                </FieldLabel>
                <Input
                  placeholder="/v1/"
                  {...register(`injectionRuleRows.${index}.pathPrefixes` as const)}
                />
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => rules.remove(index)}
              className="text-destructive mt-5"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>
      {(errors.injectionCredentialRows || errors.injectionRuleRows) && (
        <p className="text-destructive text-xs">{t("envs.form.secretInjection.hasErrors")}</p>
      )}
      <p className="text-muted-foreground text-xs">
        {t("envs.form.secretInjection.usableNotReadable")}
      </p>
    </section>
  )
}

// ─── Network policy section ─────────────────────────────────────────────────

interface NetworkPolicySectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
}

function NetworkPolicySection({ control, register, errors }: NetworkPolicySectionProps) {
  const { t } = useTranslation()
  const mode = useWatch({ control, name: "networkPolicyMode" })
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.networkPolicy")}
        </h3>
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.networkPolicy.hint")}</p>
      </div>

      <Field>
        <FieldLabel>{t("envs.form.networkPolicy.mode")}</FieldLabel>
        <Controller
          control={control}
          name="networkPolicyMode"
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="unrestricted">
                  {t("envs.form.networkPolicy.modeUnrestricted")}
                </SelectItem>
                <SelectItem value="disable">{t("envs.form.networkPolicy.modeDisable")}</SelectItem>
                <SelectItem value="allowlist">
                  {t("envs.form.networkPolicy.modeAllowlist")}
                </SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        <FieldDescription>{t("envs.form.networkPolicy.modeDescription")}</FieldDescription>
      </Field>

      {mode === "unrestricted" && (
        <p className="text-muted-foreground rounded-md border border-dashed p-3 text-xs">
          {t("envs.form.networkPolicy.unrestrictedPrivateNote")}
        </p>
      )}

      {mode === "allowlist" && (
        <>
          <Field>
            <FieldLabel htmlFor="np-domains">
              {t("envs.form.networkPolicy.allowedDomains")}
            </FieldLabel>
            <Textarea
              id="np-domains"
              rows={3}
              placeholder={"pypi.org\n*.pythonhosted.org"}
              className="font-mono text-sm"
              {...register("allowedDomains")}
            />
            {errors.allowedDomains && (
              <FieldError>{t(errors.allowedDomains.message as never)}</FieldError>
            )}
            <FieldDescription>
              {t("envs.form.networkPolicy.allowedDomainsDescription")}
            </FieldDescription>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="np-allow-cidr">
                {t("envs.form.networkPolicy.allowedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-allow-cidr"
                rows={2}
                placeholder="8.8.8.8/32"
                className="font-mono text-sm"
                {...register("allowedCIDRs")}
              />
              {errors.allowedCIDRs && (
                <FieldError>{t(errors.allowedCIDRs.message as never)}</FieldError>
              )}
            </Field>
            <Field>
              <FieldLabel htmlFor="np-deny-cidr">
                {t("envs.form.networkPolicy.deniedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-deny-cidr"
                rows={2}
                placeholder="1.2.3.4/32"
                className="font-mono text-sm"
                {...register("deniedCIDRs")}
              />
              {errors.deniedCIDRs && (
                <FieldError>{t(errors.deniedCIDRs.message as never)}</FieldError>
              )}
            </Field>
          </div>
        </>
      )}

      {mode !== "unrestricted" && (
        <Controller
          control={control}
          name="allowPrivateNetworks"
          render={({ field }) => (
            <div className="flex items-center justify-between rounded-md border p-3">
              <div className="space-y-0.5 pr-3">
                <FieldLabel>{t("envs.form.networkPolicy.allowPrivateNetworks")}</FieldLabel>
                <FieldDescription>
                  {t("envs.form.networkPolicy.allowPrivateNetworksDescription")}
                </FieldDescription>
              </div>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </div>
          )}
        />
      )}
    </section>
  )
}

interface UpdateStrategySectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
}

/**
 * Panel body only — the auto-update toggle itself lives in the accordion header
 * so its state is readable without expanding anything.
 */
function UpdateStrategySection({ control, register }: UpdateStrategySectionProps) {
  const { t } = useTranslation()
  const autoUpdate = useWatch({ control, name: "autoUpdate" })
  return (
    <section className="space-y-3 pb-2">
      <p className="text-muted-foreground text-xs">
        {t("envs.form.updateStrategy.autoUpdateDescription")}
      </p>

      {autoUpdate ? (
        <Field>
          <FieldLabel htmlFor="us-max-unavailable">
            {t("envs.form.updateStrategy.maxUnavailable")}
          </FieldLabel>
          <Input id="us-max-unavailable" placeholder="20%" {...register("maxUnavailable")} />
          <FieldDescription>
            {t("envs.form.updateStrategy.maxUnavailableDescription")}
          </FieldDescription>
        </Field>
      ) : (
        <p className="text-muted-foreground rounded-md border border-dashed p-3 text-xs">
          {t("envs.form.updateStrategy.hint")}
        </p>
      )}
    </section>
  )
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
