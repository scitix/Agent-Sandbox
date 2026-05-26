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

// @docs-example — This file is referenced as the canonical form pattern example in CLAUDE.md.
// If you rename, move, or significantly restructure this file, update the CLAUDE.md references too.
// Key sections for docs (line numbers may shift — keep in sync):
//   Schema definition:       see `const schema = z.object({...})`
//   Inner form + useQuery:   see `function CreateSandboxForm`
//   Object Combobox pattern: see the `poolName` Controller block
//   Plain input pattern:     see the `image` Field block
//   Dialog shell:            see `export function CreateSandboxDialog`

"use client"

import { useState } from "react"
import { useForm, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Loader2, Plus, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { toast } from "sonner"
import { useQuery } from "@tanstack/react-query"
import { useCreateSandbox } from "@/lib/queries/sandbox"
import { envsQueryOptions } from "@/lib/queries"
import type { AgentSandboxEnv } from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// ─── Helpers ──────────────────────────────────────────────────────────────────

// Sum idle replicas across every scaling group on the Env's status. The
// router dispatches Sandbox.Create requests to the first pool with capacity,
// so the user only cares about the aggregate — picking the first group (as
// the table on the Envs page does) would under-count multi-group Envs.
function totalIdleFor(env: AgentSandboxEnv): number {
  let sum = 0
  for (const g of env.status?.scalingGroups ?? []) {
    sum += g.totalIdle ?? 0
  }
  return sum
}

// ─── Schema ───────────────────────────────────────────────────────────────────

const schema = z.object({
  poolName: z.string().min(1, "Pool name is required"),
  image: z.string().optional(),
  startupTimeout: z
    .string()
    .regex(/^(\d+[smh])?$/, "Format: 30s, 5m, 1h (or leave empty)")
    .optional(),
  idleTimeout: z
    .string()
    .regex(/^(\d+[smh])?$/, "Format: 30s, 5m, 1h (or leave empty)")
    .optional(),
})

type FormData = z.infer<typeof schema>

// ─── Inner form component ────────────────────────────────────────────────────
// Rendered only when the dialog is open, so useQuery only runs then.

interface CreateSandboxFormProps {
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

function CreateSandboxForm({ onOpenChange, onCreated }: CreateSandboxFormProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useCreateSandbox()
  const [createTimeout, setCreateTimeout] = useState(false)

  // Fetch envs fresh on every open — component mounts only when dialog is
  // open. The backend's envRouter (sandbox_service.go) resolves an Env name
  // passed as body.poolName to one of its member pools, so the form ships
  // the selected env name in the legacy poolName slot.
  const { data: envs } = useQuery(envsQueryOptions())

  const {
    register,
    handleSubmit,
    reset,
    control,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
  })

  const onSubmit = (data: FormData) => {
    setCreateTimeout(false)
    mutate(
      {
        body: {
          poolName: data.poolName,
          image: data.image || undefined,
          startupTimeout: data.startupTimeout || undefined,
          idleTimeout: data.idleTimeout || undefined,
        },
      },
      {
        onSuccess: () => {
          toast.success(t("sandboxes.createdSuccess"))
          reset()
          setCreateTimeout(false)
          onOpenChange(false)
          onCreated?.()
        },
        onError: (error: unknown) => {
          // SANDBOX_CREATE_TIMEOUT: Ingress gateway (fixed 60s) dropped TCP before
          // Go backend finished. Middleware toast is suppressed for this errorCode.
          // Show an in-dialog amber banner so the user knows to check the list.
          const err = error as Error & { errorCode?: string }
          if (err?.errorCode === "SANDBOX_CREATE_TIMEOUT") {
            setCreateTimeout(true)
          }
          // All other errors: middleware already showed a toast; keep dialog open.
        },
      },
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <form
        id="create-sandbox-form"
        onSubmit={handleSubmit(onSubmit)}
        className="flex flex-1 flex-col overflow-hidden"
      >
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-1 py-2">
          <Field data-invalid={!!errors.poolName}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("sandboxes.form.envName")} <span className="text-destructive">*</span>
            </FieldLabel>
            <Controller
              control={control}
              name="poolName"
              render={({ field, fieldState }) => {
                const selectedEnv = envs?.find((e) => e.name === field.value)
                return (
                  <Combobox
                    autoHighlight
                    value={selectedEnv ?? null}
                    onValueChange={(val) => field.onChange(val?.name ?? "")}
                    items={envs}
                    itemToStringLabel={(e) => e.name}
                  >
                    <ComboboxInput
                      aria-invalid={fieldState.invalid}
                      placeholder={t("common.search")}
                      className="h-9 font-mono text-sm"
                    />
                    <ComboboxContent>
                      <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
                      <ComboboxList>
                        {(e) => {
                          const idle = totalIdleFor(e)
                          return (
                            <ComboboxItem key={e.name} value={e}>
                              <span>{e.name}</span>
                              <span
                                className={cn(
                                  "ml-auto font-mono text-xs",
                                  idle > 0
                                    ? "text-green-700 dark:text-green-400"
                                    : "text-muted-foreground",
                                )}
                              >
                                {t("sandboxes.form.envIdle", { count: idle })}
                              </span>
                            </ComboboxItem>
                          )
                        }}
                      </ComboboxList>
                    </ComboboxContent>
                  </Combobox>
                )
              }}
            />
            <FieldError errors={[errors.poolName]} className="font-mono text-xs" />
            <FieldDescription>{t("sandboxes.form.selectEnv")}</FieldDescription>
          </Field>

          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("sandboxes.form.image")}
            </FieldLabel>
            <Input
              {...register("image")}
              placeholder="docker.io/myorg/myimage:latest"
              className="border-border bg-background h-9 font-mono text-sm"
            />
            <FieldDescription>{t("sandboxes.form.optionalImage")}</FieldDescription>
          </Field>

          <div className="border-border rounded border">
            <Accordion>
              <AccordionItem value="advanced">
                <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-xs font-bold tracking-[0.12em] uppercase hover:no-underline">
                  {t("common.advanced")}
                </AccordionTrigger>
                <AccordionContent className="px-3">
                  <div className="flex flex-col gap-4 pb-2">
                    <Field data-invalid={!!errors.idleTimeout}>
                      <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                        {t("sandboxes.form.idleTimeout")}
                      </FieldLabel>
                      <Input
                        {...register("idleTimeout")}
                        placeholder="e.g. 30s, 5m, 1h"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      <FieldError errors={[errors.idleTimeout]} className="font-mono text-xs" />
                      <FieldDescription>{t("sandboxes.form.idleTimeoutDesc")}</FieldDescription>
                    </Field>
                    <Field data-invalid={!!errors.startupTimeout}>
                      <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                        {t("sandboxes.form.startupTimeout")}
                      </FieldLabel>
                      <Input
                        {...register("startupTimeout")}
                        placeholder="e.g. 30s, 2m"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      <FieldError errors={[errors.startupTimeout]} className="font-mono text-xs" />
                      <FieldDescription>{t("sandboxes.form.startupTimeoutDesc")}</FieldDescription>
                    </Field>
                  </div>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </div>
        </div>
      </form>

      <DialogFooter className="mt-2 gap-2 px-1 pb-1">
        {createTimeout && (
          <div className="w-full rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200">
            {t("sandboxes.createTimeoutHint")}
          </div>
        )}
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            reset()
            onOpenChange(false)
          }}
          className="font-mono text-xs tracking-wider uppercase"
        >
          <X className="mr-1.5 h-3.5 w-3.5" />
          {t("common.cancel")}
        </Button>
        <Button
          type="submit"
          form="create-sandbox-form"
          disabled={isMutating}
          className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
        >
          {isMutating ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <Plus className="mr-1.5 h-3.5 w-3.5" />
          )}
          {t("common.create")}
        </Button>
      </DialogFooter>
    </div>
  )
}

// ─── Dialog shell ─────────────────────────────────────────────────────────────

interface CreateSandboxDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

export function CreateSandboxDialog({ open, onOpenChange, onCreated }: CreateSandboxDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="border-border bg-card flex max-h-[85dvh] flex-col gap-0 p-0 sm:max-w-md">
        <DialogHeader className="border-border border-b px-6 py-4">
          <DialogTitle className="font-mono text-sm tracking-wide uppercase">
            {t("sandboxes.createTitle")}
          </DialogTitle>
        </DialogHeader>
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-6 pt-2 pb-4">
          {open && <CreateSandboxForm onOpenChange={onOpenChange} onCreated={onCreated} />}
        </div>
      </DialogContent>
    </Dialog>
  )
}
