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
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { CheckCheck, Copy, KeyRound, Plus, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { KeyRevealModal, type KeyRevealResult } from "@/components/api-keys/key-reveal-modal"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Field, FieldLabel } from "@/components/ui/field"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import { authAtom } from "@/lib/atoms"
import type { AgentboxApiKey } from "@/lib/api/client"
import {
  globalApiKeysQueryOptions,
  useCreateGlobalApiKey,
  useDeleteGlobalApiKey,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { ExpiresAtPicker } from "@/components/api-keys/expires-at-picker"

function formatDate(dateStr?: string): string {
  if (!dateStr) return "Never"
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleDateString("en-US", { year: "numeric", month: "short", day: "numeric" })
}

// ─── Token Block ──────────────────────────────────────────────────────────────

function TokenBlock({ raw }: { raw: string }) {
  const [copied, setCopied] = useState(false)
  const masked = raw.length > 17 ? raw.slice(0, 9) + "..." + raw.slice(-4) : raw

  const handleCopy = () => {
    navigator.clipboard.writeText(raw)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className="bg-muted/50 border-border flex items-center gap-2 rounded-md border px-3 py-2">
      <pre className="text-foreground min-w-0 flex-1 overflow-hidden font-mono text-xs leading-none tracking-wide">
        {masked}
      </pre>
      <button
        onClick={handleCopy}
        className={cn(
          "text-muted-foreground hover:text-foreground shrink-0 rounded p-0.5 transition-colors",
          copied && "text-green-500 hover:text-green-500",
        )}
        title={copied ? "Copied!" : "Copy API key"}
      >
        {copied ? <CheckCheck className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}

// ─── Create Dialog ────────────────────────────────────────────────────────────

function CreateApiKeyDialog({
  open,
  onOpenChange,
  onKeyCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onKeyCreated: (result: KeyRevealResult) => void
}) {
  const { mutate: createKey, isPending: isMutating } = useCreateGlobalApiKey()
  const { t } = useTranslation()
  const [description, setDescription] = useState("")
  const [expiresAt, setExpiresAt] = useState<Date | undefined>(undefined)

  const handleCreate = () => {
    createKey(
      {
        description: description || undefined,
        expiresAt: expiresAt ? expiresAt.toISOString() : undefined,
      },
      {
        onSuccess: (result) => {
          setDescription("")
          setExpiresAt(undefined)
          onOpenChange(false)
          onKeyCreated({
            apiKey: result.apiKey,
            keyId: result.keyId,
            team: result.team ?? undefined,
          })
        },
      },
    )
  }

  const handleClose = () => {
    setDescription("")
    setExpiresAt(undefined)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="border-border bg-card sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm tracking-wide uppercase">
            {t("apiKeys.createTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("apiKeys.globalKeyDesc")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4 py-2">
          {/* Description */}
          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("apiKeys.form.description")}
            </FieldLabel>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g. production ML pipeline key"
              className="border-border bg-background h-9 font-mono text-sm"
            />
          </Field>

          {/* Expires At */}
          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("apiKeys.form.expiresAt")}
            </FieldLabel>
            <ExpiresAtPicker value={expiresAt} onChange={setExpiresAt} />
          </Field>

          <DialogFooter className="mt-2 gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              className="font-mono text-xs tracking-wider uppercase"
            >
              {t("common.cancel")}
            </Button>
            <Button
              onClick={handleCreate}
              disabled={isMutating}
              className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
            >
              {isMutating ? (
                <span className="flex items-center gap-1.5">
                  <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
                  {t("common.loading")}
                </span>
              ) : (
                <span className="flex items-center gap-1.5">
                  <Plus className="h-3.5 w-3.5" />
                  {t("common.create")}
                </span>
              )}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// ─── Revoke Dialog ────────────────────────────────────────────────────────────

function RevokeApiKeyDialog({
  apiKey,
  onOpenChange,
}: {
  apiKey: AgentboxApiKey | null
  onOpenChange: (open: boolean) => void
}) {
  const { mutate: deleteKey, isPending: isMutating } = useDeleteGlobalApiKey()
  const { t } = useTranslation()

  const handleRevoke = () => {
    if (!apiKey) return
    deleteKey(apiKey.keyId, {
      onSuccess: () => {
        toast.success(t("apiKeys.revokedSuccess"))
        onOpenChange(false)
      },
    })
  }

  return (
    <Dialog
      open={!!apiKey}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <DialogContent className="border-border bg-card sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-destructive font-mono text-sm tracking-wide uppercase">
            {t("apiKeys.revokeTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("apiKeys.revokeDescription")}
          </DialogDescription>
        </DialogHeader>

        {apiKey && (
          <div className="flex flex-col gap-2 py-2">
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("apiKeys.col.keyId")}
              </div>
              <code className="text-foreground font-mono text-sm">{apiKey.keyId}</code>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("apiKeys.col.team")}
                </div>
                <span className="font-mono text-xs">{apiKey.team ?? "---"}</span>
              </div>
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("apiKeys.col.user")}
                </div>
                <span className="font-mono text-xs">{apiKey.user ?? "---"}</span>
              </div>
            </div>
          </div>
        )}

        <DialogFooter className="gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isMutating}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={handleRevoke}
            disabled={isMutating}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {isMutating ? t("common.loading") : t("common.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export default function ApiKeysPage() {
  const auth = useAtomValue(authAtom)
  const { t } = useTranslation()
  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<AgentboxApiKey | null>(null)
  const [createdKey, setCreatedKey] = useState<KeyRevealResult | null>(null)

  const keysOptions = globalApiKeysQueryOptions()
  const { data: apiKeys, isLoading, isFetching } = useQuery(keysOptions)
  const qc = useQueryClient()

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="p-6">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <p className="text-muted-foreground text-sm">{t("admin.manageApiKeys")}</p>
            {auth && (
              <p className="text-muted-foreground mt-1 font-mono text-xs">
                {t("apiKeys.apiKeyScopedTo", {
                  scope: auth.team ? `team:${auth.team}` : (auth.user ?? "---"),
                })}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => void qc.refetchQueries({ queryKey: keysOptions.queryKey })}
              className="text-muted-foreground h-7 w-7"
              disabled={isFetching}
            >
              <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`} />
            </Button>
            <Button
              onClick={() => setCreateOpen(true)}
              className="bg-foreground text-background hover:bg-foreground/90 gap-2 font-mono text-xs tracking-wider uppercase"
            >
              <Plus className="h-3.5 w-3.5" />
              {t("apiKeys.createTitle")}
            </Button>
          </div>
        </div>

        {isLoading ? (
          <div className="flex h-40 items-center justify-center">
            <div className="bg-brand h-1 w-24 animate-pulse" />
          </div>
        ) : !apiKeys || apiKeys.length === 0 ? (
          <div className="border-border bg-card flex flex-col items-center justify-center border py-16 text-center">
            <KeyRound className="text-muted-foreground mb-3 h-8 w-8" />
            <p className="text-muted-foreground font-mono text-sm tracking-wider uppercase">
              {t("apiKeys.noApiKeys")}
            </p>
            <p className="text-muted-foreground mt-1 text-xs">{t("apiKeys.createFirstApiKey")}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {apiKeys.map((key) => (
              <div
                key={key.keyId}
                className="border-border bg-card rounded-lg border shadow-sm transition-shadow hover:shadow-md"
              >
                <div className="flex items-start gap-4 p-4">
                  {/* Key icon */}
                  <div className="bg-muted mt-0.5 shrink-0 rounded-md p-2">
                    <KeyRound className="text-muted-foreground h-4 w-4" />
                  </div>

                  {/* Main content */}
                  <div className="min-w-0 flex-1 space-y-2.5">
                    {/* Row 1: ID + badges */}
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-foreground font-mono text-sm font-semibold">
                        {key.keyId}
                      </span>
                      <Badge variant="outline" className="font-mono text-xs">
                        {key.role}
                      </Badge>
                      {key.syncSource === "global" ? (
                        <Badge variant="secondary" className="font-mono text-xs">
                          {t("status.global")}
                        </Badge>
                      ) : (
                        <Badge
                          variant="outline"
                          className="text-muted-foreground font-mono text-xs"
                        >
                          {t("status.local")}
                        </Badge>
                      )}
                    </div>

                    {/* Row 2: team / user */}
                    {(key.team || key.user) && (
                      <div className="flex flex-wrap items-center gap-4">
                        {key.team && (
                          <span className="text-muted-foreground font-mono text-[12px]">
                            team: <span className="text-foreground font-semibold">{key.team}</span>
                          </span>
                        )}
                        {key.user && (
                          <span className="text-muted-foreground font-mono text-[12px]">
                            user: <span className="text-foreground font-semibold">{key.user}</span>
                          </span>
                        )}
                      </div>
                    )}

                    {/* Row 3: description */}
                    {key.description && (
                      <p className="text-muted-foreground text-xs leading-relaxed">
                        {key.description}
                      </p>
                    )}

                    {/* Row 4: raw token block */}
                    {key.rawToken && <TokenBlock raw={key.rawToken} />}

                    {/* Row 5: dates */}
                    <div className="flex flex-wrap items-center gap-4">
                      <span className="text-muted-foreground font-mono text-[11px]">
                        <span className="tracking-wide uppercase">{t("apiKeys.col.issuedAt")}</span>
                        {" · "}
                        {formatDate(key.issuedAt)}
                      </span>
                      {key.expiresAt && (
                        <span className="text-muted-foreground font-mono text-[11px]">
                          <span className="tracking-wide uppercase">
                            {t("apiKeys.col.expiresAt")}
                          </span>
                          {" · "}
                          {formatDate(key.expiresAt)}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Delete button */}
                  {key.syncSource === "global" && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground hover:text-destructive mt-0.5 shrink-0"
                      onClick={() => setRevokeTarget(key)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <CreateApiKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onKeyCreated={setCreatedKey}
      />
      <RevokeApiKeyDialog
        apiKey={revokeTarget}
        onOpenChange={(open) => {
          if (!open) setRevokeTarget(null)
        }}
      />
      {createdKey && <KeyRevealModal result={createdKey} onClose={() => setCreatedKey(null)} />}
    </div>
  )
}
