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
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import type { CreateApiKeyResponse } from "@/lib/api/client"
import { useCreateApiKey } from "@/lib/queries"
import { useQuery } from "@tanstack/react-query"
import { adminNamespacesQueryOptions } from "@/lib/queries"
import { KeyRevealModal } from "./key-reveal-modal"
import { ExpiresAtPicker } from "./expires-at-picker"
import { useTranslation } from "@/lib/i18n"

const schema = z.object({
  namespace: z.string().min(1, "Namespace is required"),
  role: z.enum(["tenant", "admin"]),
  user: z.string().optional(),
  team: z.string().optional(),
  description: z.string().optional(),
  expiresAt: z.date().optional(),
})

type FormData = z.infer<typeof schema>

interface CreateApiKeyDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

// ─── Inner form — only rendered when dialog is open, so useQuery hooks fire ─────

interface CreateApiKeyFormProps {
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
  onKeyCreated: (result: CreateApiKeyResponse) => void
}

function CreateApiKeyForm({ onOpenChange, onCreated, onKeyCreated }: CreateApiKeyFormProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useCreateApiKey()

  // Fetch options from backend
  const { data: namespaces } = useQuery(adminNamespacesQueryOptions())

  const {
    register,
    handleSubmit,
    reset,
    control,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: { role: "tenant" },
  })

  const onSubmit = (data: FormData) => {
    let expiresAt: string | undefined
    if (data.expiresAt) {
      expiresAt = data.expiresAt.toISOString()
    }
    mutate(
      {
        body: {
          namespace: data.namespace,
          user: data.user || undefined,
          team: data.team || undefined,
          description: data.description || undefined,
          expiresAt,
        },
      },
      {
        onSuccess: (result) => {
          reset()
          onOpenChange(false)
          if (result) onKeyCreated(result as CreateApiKeyResponse)
          onCreated?.()
        },
      },
    )
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4 py-2">
      {/* Namespace — Combobox */}
      <Field data-invalid={!!errors.namespace}>
        <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("apiKeys.form.namespace")} <span className="text-destructive">*</span>
        </FieldLabel>
        <Controller
          control={control}
          name="namespace"
          render={({ field, fieldState }) => (
            <Combobox
              value={field.value ?? null}
              onValueChange={(val) => field.onChange(val)}
              items={namespaces}
            >
              <ComboboxInput
                aria-invalid={fieldState.invalid}
                placeholder={t("apiKeys.form.selectNamespace")}
                className="h-9 font-mono text-sm"
              />
              <ComboboxContent>
                <ComboboxEmpty>{t("apiKeys.form.noNamespacesFound")}</ComboboxEmpty>
                <ComboboxList>
                  {(ns) => (
                    <ComboboxItem key={ns} value={ns}>
                      <span className="font-mono">{ns}</span>
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
          )}
        />
        <FieldError errors={[errors.namespace]} className="font-mono text-xs" />
        <FieldDescription>{t("apiKeys.form.namespaceDesc")}</FieldDescription>
      </Field>

      {/* Role — Combobox */}
      <Field>
        <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("apiKeys.form.role")} <span className="text-destructive">*</span>
        </FieldLabel>
        <Controller
          control={control}
          name="role"
          render={({ field }) => (
            <Combobox
              value={field.value}
              onValueChange={(val) => field.onChange(val as "tenant" | "admin")}
            >
              <ComboboxInput
                placeholder={t("apiKeys.form.selectRole")}
                className="h-9 font-mono text-sm"
              />
              <ComboboxContent>
                <ComboboxList>
                  <ComboboxItem value="tenant">
                    <span>tenant</span>
                    <span className="text-muted-foreground ml-auto text-xs">
                      {t("apiKeys.form.regularUser")}
                    </span>
                  </ComboboxItem>
                  <ComboboxItem value="admin">
                    <span>admin</span>
                    <span className="text-muted-foreground ml-auto text-xs">
                      {t("apiKeys.form.fullAccess")}
                    </span>
                  </ComboboxItem>
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
          )}
        />
        <FieldDescription>{t("apiKeys.form.roleDesc")}</FieldDescription>
      </Field>

      {/* Team + User — free-form text inputs */}
      <div className="grid grid-cols-2 gap-3">
        {/* Team */}
        <Field>
          <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("apiKeys.form.team")}
          </FieldLabel>
          <Input
            {...register("team")}
            placeholder={t("apiKeys.form.selectTeam")}
            className="border-border bg-background h-9 font-mono text-sm"
          />
        </Field>

        {/* User */}
        <Field>
          <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("apiKeys.form.user")}
          </FieldLabel>
          <Input
            {...register("user")}
            placeholder={t("apiKeys.form.selectUser")}
            className="border-border bg-background h-9 font-mono text-sm"
          />
        </Field>
      </div>
      <FieldDescription>{t("apiKeys.form.scopeDesc")}</FieldDescription>

      {/* Description */}
      <Field>
        <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("apiKeys.form.description")}
        </FieldLabel>
        <Input
          {...register("description")}
          placeholder={t("apiKeys.form.descriptionPlaceholder")}
          className="border-border bg-background h-9 font-mono text-sm"
        />
        <FieldDescription>{t("apiKeys.form.descriptionOptional")}</FieldDescription>
      </Field>

      {/* Expires At */}
      <Field>
        <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("apiKeys.form.expiresAt")}{" "}
          <span className="text-muted-foreground">{t("apiKeys.form.optional")}</span>
        </FieldLabel>
        <Controller
          control={control}
          name="expiresAt"
          render={({ field }) => (
            <ExpiresAtPicker value={field.value} onChange={field.onChange} />
          )}
        />
        <FieldDescription>{t("apiKeys.form.expiresAtDesc")}</FieldDescription>
      </Field>

      <DialogFooter className="mt-2 gap-2">
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
    </form>
  )
}

// ─── Outer shell — controls Dialog open state; inner form only mounts when open ─

export function CreateApiKeyDialog({ open, onOpenChange, onCreated }: CreateApiKeyDialogProps) {
  const { t } = useTranslation()
  const [createdKey, setCreatedKey] = useState<CreateApiKeyResponse | null>(null)

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="border-border bg-card sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm tracking-wide uppercase">
              {t("apiKeys.createTitle")}
            </DialogTitle>
          </DialogHeader>
          <CreateApiKeyForm
            onOpenChange={onOpenChange}
            onCreated={onCreated}
            onKeyCreated={setCreatedKey}
          />
        </DialogContent>
      </Dialog>

      {createdKey && <KeyRevealModal result={createdKey} onClose={() => setCreatedKey(null)} />}
    </>
  )
}
