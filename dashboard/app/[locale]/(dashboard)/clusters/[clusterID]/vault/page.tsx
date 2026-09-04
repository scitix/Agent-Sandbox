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

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { CheckCheck, Copy, Plus, RotateCw, Trash2, Vault } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Field, FieldLabel } from "@/components/ui/field"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ApiKeyRequiredNotice } from "@/components/custom/api-key-required-notice"
import { globalApiKeysQueryOptions, pickUsableApiKey } from "@/lib/queries"
import { impersonationAtom } from "@/lib/atoms"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"
import {
  SECRET_NAME_RE,
  secretPlaceholder,
  useCreateSecret,
  useDeleteSecret,
  useRotateSecret,
  useSecrets,
  type SecretInfo,
} from "@/lib/queries/vault"

/**
 * Attributes that keep a browser's password manager out of these fields.
 *
 * A name box next to a masked box looks exactly like a login form, so Chrome
 * fills the first with the saved username and offers a saved password for the
 * second — which is how a secret ends up named after whoever is logged in.
 * `autoComplete="off"` alone does not stop it: browsers ignore it on
 * credential-shaped fields, and only the "this is a new password" hint reliably
 * suppresses the saved-credential dropdown. The data-* attributes are the
 * opt-outs 1Password and LastPass read.
 */
const noAutofill = {
  autoComplete: "off",
  autoCorrect: "off",
  autoCapitalize: "off",
  spellCheck: false,
  "data-1p-ignore": true,
  "data-lpignore": "true",
  "data-form-type": "other",
} as const

/** As above, for the masked value field, which needs the stronger hint. */
const noAutofillSecret = { ...noAutofill, autoComplete: "new-password" } as const

function formatDate(value?: string): string {
  if (!value) return "—"
  const d = new Date(value)
  if (isNaN(d.getTime())) return value
  return d.toLocaleString()
}

/**
 * The reference a network rule uses to reach this secret. It is the only thing
 * about a secret that can be copied — the value itself is write-only and no
 * read surface returns it.
 */
function PlaceholderBlock({ name }: { name: string }) {
  const [copied, setCopied] = useState(false)
  const placeholder = secretPlaceholder(name)

  return (
    <button
      type="button"
      className="bg-muted/50 border-border hover:bg-muted flex items-center gap-2 rounded-md border px-2 py-1 font-mono text-xs"
      onClick={() => {
        navigator.clipboard.writeText(placeholder)
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
    >
      <span className="truncate">{placeholder}</span>
      {copied ? (
        <CheckCheck className="h-3 w-3 shrink-0 text-green-600" />
      ) : (
        <Copy className="text-muted-foreground h-3 w-3 shrink-0" />
      )}
    </button>
  )
}

export default function VaultPage() {
  const { t } = useTranslation()
  const clusterID = useClusterID()

  // The vault is on the E2B surface, whose auth middleware takes API keys only
  // — never the session JWT. So the console picks a key on the user's behalf,
  // the same way creating a sandbox does. Reading it off the JWT would only
  // work for callers whose token happens to carry one, which an OIDC session
  // does not.
  const { data: apiKeys, isLoading: keysLoading } = useQuery(globalApiKeysQueryOptions())
  const apiKey = useMemo(() => pickUsableApiKey(apiKeys)?.rawToken ?? "", [apiKeys])

  // A vault belongs to (namespace, user), so an admin using the user switcher
  // must be shown the switched-to user's credentials — not their own under a
  // different heading.
  const impersonation = useAtomValue(impersonationAtom)
  const opts = { clusterID, apiKey, impersonate: impersonation }
  const { data: secrets, isLoading } = useSecrets(opts, true)
  const createSecret = useCreateSecret(opts)
  const rotateSecret = useRotateSecret(opts)
  const deleteSecret = useDeleteSecret(opts)

  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState("")
  const [value, setValue] = useState("")
  const [rotateTarget, setRotateTarget] = useState<SecretInfo | null>(null)
  const [rotateValue, setRotateValue] = useState("")
  const [deleteTarget, setDeleteTarget] = useState<SecretInfo | null>(null)

  // Only claim a key is missing once the lookup has actually finished —
  // otherwise every load flashes "no API key" before the list arrives.
  if (keysLoading) {
    return <p className="text-muted-foreground p-4 text-sm">{t("common.loading")}</p>
  }
  if (!apiKey) {
    return <ApiKeyRequiredNotice description={t("vault.apiKeyRequired")} />
  }

  const nameError = name && !SECRET_NAME_RE.test(name) ? t("vault.create.nameInvalid") : undefined

  const handleCreate = () => {
    createSecret.mutate(
      { name, value },
      {
        onSuccess: () => {
          toast.success(t("vault.create.success"))
          setCreateOpen(false)
          setName("")
          setValue("")
        },
      },
    )
  }

  const handleRotate = () => {
    if (!rotateTarget) return
    rotateSecret.mutate(
      { secretID: rotateTarget.secretID, value: rotateValue },
      {
        onSuccess: () => {
          toast.success(t("vault.rotate.success"))
          setRotateTarget(null)
          setRotateValue("")
        },
      },
    )
  }

  return (
    <div className="flex flex-1 flex-col gap-4 p-4">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="flex items-center gap-2 text-lg font-semibold">
            <Vault className="h-4 w-4" />
            {t("vault.title")}
          </h1>
          <p className="text-muted-foreground max-w-2xl text-xs">{t("vault.description")}</p>
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="mr-1 h-4 w-4" />
          {t("vault.create.action")}
        </Button>
      </div>

      {isLoading ? (
        <p className="text-muted-foreground p-4 text-sm">{t("common.loading")}</p>
      ) : !secrets?.length ? (
        <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
          <Vault className="h-8 w-8" />
          <p className="text-sm">{t("vault.empty.title")}</p>
          <p className="max-w-md text-xs">{t("vault.empty.description")}</p>
        </div>
      ) : (
        <div className="border-border overflow-x-auto rounded-md border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground text-xs">
              <tr>
                <th className="px-3 py-2 text-left font-medium">{t("vault.columns.name")}</th>
                <th className="px-3 py-2 text-left font-medium">
                  {t("vault.columns.placeholder")}
                </th>
                <th className="px-3 py-2 text-left font-medium">{t("vault.columns.version")}</th>
                <th className="px-3 py-2 text-left font-medium">{t("vault.columns.updatedAt")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("vault.columns.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {secrets.map((s) => (
                <tr key={s.secretID} className="border-border border-t">
                  <td className="px-3 py-2 font-medium">{s.name}</td>
                  <td className="px-3 py-2">
                    <PlaceholderBlock name={s.name} />
                  </td>
                  <td className="px-3 py-2">
                    <Badge variant="secondary">v{s.currentVersion}</Badge>
                  </td>
                  <td className="text-muted-foreground px-3 py-2 text-xs">
                    {formatDate(s.updatedAt)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        setRotateTarget(s)
                        setRotateValue("")
                      }}
                    >
                      <RotateCw className="mr-1 h-3 w-3" />
                      {t("vault.rotate.action")}
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setDeleteTarget(s)}>
                      <Trash2 className="text-destructive h-3 w-3" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("vault.create.title")}</DialogTitle>
            <DialogDescription>{t("vault.create.description")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <Field>
              <FieldLabel>{t("vault.create.nameLabel")}</FieldLabel>
              <Input
                {...noAutofill}
                // Not "name": a field literally called name is what the
                // password manager latches onto.
                name="vault-entry"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="openai-api-key"
              />
              {nameError ? (
                <p className="text-destructive text-xs">{nameError}</p>
              ) : name ? (
                <p className="text-muted-foreground font-mono text-xs">
                  {secretPlaceholder(name.toLowerCase())}
                </p>
              ) : null}
            </Field>
            <Field>
              <FieldLabel>{t("vault.create.valueLabel")}</FieldLabel>
              <Input
                {...noAutofillSecret}
                name="vault-value"
                type="password"
                value={value}
                onChange={(e) => setValue(e.target.value)}
              />
              <p className="text-muted-foreground text-xs">{t("vault.valueWriteOnly")}</p>
            </Field>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              disabled={!name || !value || Boolean(nameError) || createSecret.isPending}
              onClick={handleCreate}
            >
              {t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotate */}
      <Dialog open={Boolean(rotateTarget)} onOpenChange={(o) => !o && setRotateTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("vault.rotate.title")}</DialogTitle>
            <DialogDescription>{t("vault.rotate.description")}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel>{t("vault.create.valueLabel")}</FieldLabel>
            <Input
              {...noAutofillSecret}
              name="vault-rotate-value"
              type="password"
              value={rotateValue}
              onChange={(e) => setRotateValue(e.target.value)}
            />
          </Field>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRotateTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button disabled={!rotateValue || rotateSecret.isPending} onClick={handleRotate}>
              {t("vault.rotate.action")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete */}
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("vault.delete.title")}</DialogTitle>
            <DialogDescription>{t("vault.delete.description")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={deleteSecret.isPending}
              onClick={() => {
                if (!deleteTarget) return
                deleteSecret.mutate(deleteTarget.secretID, {
                  onSuccess: () => {
                    toast.success(t("vault.delete.success"))
                    setDeleteTarget(null)
                  },
                })
              }}
            >
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
