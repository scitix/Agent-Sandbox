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
import { Upload, Loader2, CheckCircle2, XCircle, AlertTriangle } from "lucide-react"
import { toast } from "sonner"
import { parse, parseAllDocuments } from "yaml"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { basePath, getToken } from "@/lib/api/client"
import { useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "@/lib/i18n"

// ─── Types ───────────────────────────────────────────────────────────────────

interface ParsedKey {
  tokenHash: string
  hashPrefix: string
  namespace: string
  user: string
  team: string
  role: string
  description: string
  quotaURL: string
  issuedAt: string
  expiresAt: string
  secretName: string
}

type ImportStatus = "pending" | "importing" | "success" | "skipped" | "error"

interface ImportItem {
  key: ParsedKey
  status: ImportStatus
  message?: string
}

// ─── YAML parsing helpers ────────────────────────────────────────────────────

function decodeBase64(val: unknown): string {
  if (typeof val !== "string" || !val) return ""
  try {
    return atob(val)
  } catch {
    // If it's not base64, return as-is (might be plain text)
    return val
  }
}

function parseSecretToKey(doc: unknown): ParsedKey | null {
  if (!doc || typeof doc !== "object") return null
  const obj = doc as Record<string, unknown>

  // Validate it looks like a K8s Secret
  if (obj.kind && obj.kind !== "Secret") return null

  const metadata = (obj.metadata ?? {}) as Record<string, unknown>
  const data = (obj.data ?? {}) as Record<string, string>
  const name = (metadata.name as string) ?? ""

  if (!name.startsWith("agentbox-apikey-")) return null

  const tokenHash = decodeBase64(data.token)
  if (!tokenHash) return null

  const hashPrefix = name.slice("agentbox-apikey-".length)
  if (!hashPrefix) return null

  return {
    tokenHash,
    hashPrefix,
    namespace: decodeBase64(data.namespace),
    user: decodeBase64(data.user),
    team: decodeBase64(data.team),
    role: decodeBase64(data.role) || "tenant",
    description: decodeBase64(data.description),
    quotaURL: decodeBase64(data.quotaURL),
    issuedAt: decodeBase64(data.issuedAt),
    expiresAt: decodeBase64(data.expiresAt),
    secretName: name,
  }
}

function parseYamlInput(yamlText: string): { keys: ParsedKey[]; errors: string[] } {
  const keys: ParsedKey[] = []
  const errors: string[] = []

  try {
    // Try parsing as multi-document YAML (--- separated) or SecretList
    const docs = parseAllDocuments(yamlText)

    for (const doc of docs) {
      if (doc.errors.length > 0) {
        errors.push(...doc.errors.map((e) => e.message))
        continue
      }
      const obj = doc.toJSON()
      if (!obj) continue

      // Handle SecretList (kind: SecretList or kind: List)
      if (obj.kind === "SecretList" || obj.kind === "List") {
        const items = (obj.items ?? []) as unknown[]
        for (const item of items) {
          const key = parseSecretToKey(item)
          if (key) keys.push(key)
        }
        continue
      }

      const key = parseSecretToKey(obj)
      if (key) keys.push(key)
    }

    // If multi-doc parsing found nothing, try single doc
    if (keys.length === 0 && errors.length === 0) {
      const single = parse(yamlText) as unknown
      if (single && typeof single === "object") {
        const obj = single as Record<string, unknown>
        // Maybe it's a list
        if (obj.kind === "SecretList" || obj.kind === "List") {
          const items = (obj.items ?? []) as unknown[]
          for (const item of items) {
            const key = parseSecretToKey(item)
            if (key) keys.push(key)
          }
        } else {
          const key = parseSecretToKey(single)
          if (key) keys.push(key)
        }
      }
    }
  } catch (e) {
    errors.push(`YAML parse error: ${e instanceof Error ? e.message : String(e)}`)
  }

  if (keys.length === 0 && errors.length === 0) {
    errors.push("No valid agentbox-apikey-* Secrets found in the YAML")
  }

  return { keys, errors }
}

// ─── Import Secret Dialog ────────────────────────────────────────────────────

interface ImportSecretDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ImportSecretDialog({ open, onOpenChange }: ImportSecretDialogProps) {
  const { t } = useTranslation()
  const [yamlText, setYamlText] = useState("")
  const [items, setItems] = useState<ImportItem[]>([])
  const [isImporting, setIsImporting] = useState(false)
  const [phase, setPhase] = useState<"input" | "preview" | "result">("input")
  const qc = useQueryClient()

  const handleClose = () => {
    if (isImporting) return
    setYamlText("")
    setItems([])
    setPhase("input")
    onOpenChange(false)
  }

  const handleParse = () => {
    const { keys, errors } = parseYamlInput(yamlText)
    if (errors.length > 0) {
      toast.error(errors[0])
      return
    }
    setItems(keys.map((key) => ({ key, status: "pending" })))
    setPhase("preview")
  }

  const handleImport = async () => {
    setIsImporting(true)
    const updated = [...items]

    for (let i = 0; i < updated.length; i++) {
      updated[i] = { ...updated[i], status: "importing" }
      setItems([...updated])

      const { key } = updated[i]
      try {
        // Route through the global BFF endpoint so that wsproxy broadcasts
        // the key_sync frame to ALL Worker clusters, not just the current one.
        const token = getToken()
        const response = await fetch(`${basePath}/api/global-api-keys`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({
            namespace: key.namespace || undefined,
            user: key.user || undefined,
            team: key.team || undefined,
            description: key.description || undefined,
            tokenHash: key.tokenHash,
            hashPrefix: key.hashPrefix,
            quotaURL: key.quotaURL || undefined,
            issuedAt: key.issuedAt || undefined,
            expiresAt: key.expiresAt || undefined,
          }),
        })

        if (response.ok) {
          updated[i] = { ...updated[i], status: "success", message: "Imported" }
        } else if (response.status === 409) {
          updated[i] = { ...updated[i], status: "skipped", message: "Already exists" }
        } else {
          const text = await response.text().catch(() => "")
          updated[i] = {
            ...updated[i],
            status: "error",
            message: `HTTP ${response.status}: ${text.slice(0, 100)}`,
          }
        }
      } catch (e) {
        updated[i] = {
          ...updated[i],
          status: "error",
          message: e instanceof Error ? e.message : "Unknown error",
        }
      }
      setItems([...updated])
    }

    setIsImporting(false)
    setPhase("result")

    // Invalidate the admin API keys list
    void qc.invalidateQueries({ queryKey: ["get", "/admin/api-keys"] })
  }

  const successCount = items.filter((i) => i.status === "success").length
  const skipCount = items.filter((i) => i.status === "skipped").length
  const errorCount = items.filter((i) => i.status === "error").length

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="border-border bg-card sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm tracking-wide uppercase">
            {t("apiKeys.importTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("apiKeys.importDesc")}
          </DialogDescription>
        </DialogHeader>

        {phase === "input" && (
          <>
            <Textarea
              value={yamlText}
              onChange={(e) => setYamlText(e.target.value)}
              placeholder={`apiVersion: v1\nkind: Secret\nmetadata:\n  name: agentbox-apikey-abc123...\ndata:\n  token: <base64-encoded-sha256-hash>\n  namespace: <base64>\n  user: <base64>\n  ...`}
              className="border-border bg-background max-h-100 min-h-50 font-mono text-xs"
            />
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
                onClick={handleParse}
                disabled={!yamlText.trim()}
                className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
              >
                <Upload className="mr-1.5 h-3.5 w-3.5" />
                {t("apiKeys.parse")}
              </Button>
            </DialogFooter>
          </>
        )}

        {(phase === "preview" || phase === "result") && (
          <>
            <div className="flex max-h-80 flex-col gap-1.5 overflow-y-auto">
              {items.map((item, idx) => (
                <div
                  key={idx}
                  className="border-border bg-secondary flex items-center gap-2 border px-3 py-2"
                >
                  <StatusIcon status={item.status} />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="text-foreground truncate font-mono text-xs font-semibold">
                      {item.key.secretName}
                    </span>
                    <span className="text-muted-foreground font-mono text-xs">
                      ns={item.key.namespace} user={item.key.user || "—"} team=
                      {item.key.team || "—"}
                    </span>
                  </div>
                  {item.message && (
                    <span className="text-muted-foreground shrink-0 font-mono text-xs">
                      {item.message}
                    </span>
                  )}
                </div>
              ))}
            </div>

            {phase === "result" && (
              <div className="border-border bg-secondary mt-2 border px-3 py-2 font-mono text-xs">
                <span className="text-green-600 dark:text-green-400">
                  {t("apiKeys.importResultImported", { count: String(successCount) })}
                </span>
                {" / "}
                <span className="text-muted-foreground">
                  {t("apiKeys.importResultSkipped", { count: String(skipCount) })}
                </span>
                {" / "}
                <span className={errorCount > 0 ? "text-destructive" : "text-muted-foreground"}>
                  {t("apiKeys.importResultFailed", { count: String(errorCount) })}
                </span>
              </div>
            )}

            <DialogFooter className="mt-2 gap-2">
              {phase === "preview" && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setItems([])
                      setPhase("input")
                    }}
                    className="font-mono text-xs tracking-wider uppercase"
                  >
                    {t("common.back")}
                  </Button>
                  <Button
                    onClick={handleImport}
                    disabled={isImporting || items.length === 0}
                    className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
                  >
                    {isImporting ? (
                      <>
                        <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                        {t("apiKeys.importing")}
                      </>
                    ) : (
                      <>
                        <Upload className="mr-1.5 h-3.5 w-3.5" />
                        {t("apiKeys.importCount", { count: String(items.length) })}
                      </>
                    )}
                  </Button>
                </>
              )}
              {phase === "result" && (
                <Button
                  onClick={handleClose}
                  className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
                >
                  {t("common.done")}
                </Button>
              )}
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

// ─── Status icon helper ──────────────────────────────────────────────────────

function StatusIcon({ status }: { status: ImportStatus }) {
  switch (status) {
    case "pending":
      return <div className="bg-muted-foreground/30 h-4 w-4 shrink-0 rounded-full" />
    case "importing":
      return <Loader2 className="text-brand h-4 w-4 shrink-0 animate-spin" />
    case "success":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />
    case "skipped":
      return <AlertTriangle className="h-4 w-4 shrink-0 text-amber-500" />
    case "error":
      return <XCircle className="text-destructive h-4 w-4 shrink-0" />
  }
}
