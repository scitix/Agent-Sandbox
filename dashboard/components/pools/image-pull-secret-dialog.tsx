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

import { useState, useRef } from "react"
import { Plus, Trash2, Eye, EyeOff, Upload, Download } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import { Field, FieldLabel } from "@/components/ui/field"
import { toast } from "sonner"
import { useTranslation } from "@/lib/i18n"
import type { components } from "@/lib/api/schema"

export type RegistryCredential = components["schemas"]["RegistryCredential"]
export type ImagePullSecretValue = {
  registries: RegistryCredential[]
}

const PRESET_REGISTRIES: Array<{ label: string; value: string }> = [
  { label: "Docker Hub", value: "https://index.docker.io/v1/" },
  { label: "GitHub Container Registry", value: "ghcr.io" },
  { label: "Quay", value: "quay.io" },
  { label: "Google Artifact Registry", value: "gcr.io" },
]

const CUSTOM_SENTINEL = "__custom__"

interface ImagePullSecretDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  value: ImagePullSecretValue | null
  onSave: (value: ImagePullSecretValue | null) => void
}

export function ImagePullSecretDialog({
  open,
  onOpenChange,
  value,
  onSave,
}: ImagePullSecretDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col">
        {open && <ImagePullSecretForm value={value} onSave={onSave} onOpenChange={onOpenChange} />}
      </DialogContent>
    </Dialog>
  )
}

function ImagePullSecretForm({
  value,
  onSave,
  onOpenChange,
}: {
  value: ImagePullSecretValue | null
  onSave: (value: ImagePullSecretValue | null) => void
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [registries, setRegistries] = useState<RegistryCredential[]>(() => {
    if (value && value.registries.length > 0) return value.registries.map((r) => ({ ...r }))
    return [{ registry: PRESET_REGISTRIES[0].value, username: "", password: "" }]
  })
  const [revealIndex, setRevealIndex] = useState<number | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const update = (i: number, patch: Partial<RegistryCredential>) => {
    setRegistries((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  }

  const addRegistry = () => {
    setRegistries((prev) => [...prev, { registry: "", username: "", password: "" }])
  }

  const removeRegistry = (i: number) => {
    setRegistries((prev) => prev.filter((_, idx) => idx !== i))
  }

  const handleSave = () => {
    const trimmed = registries
      .map((r) => ({
        registry: r.registry.trim(),
        username: r.username,
        password: r.password,
      }))
      .filter((r) => r.registry !== "" || r.username !== "" || r.password !== "")
    if (trimmed.length === 0) {
      toast.error(t("pools.form.imagePullSecretEmpty"))
      return
    }
    for (const r of trimmed) {
      if (!r.registry || !r.username || !r.password) {
        toast.error(t("pools.form.imagePullSecretEmpty"))
        return
      }
    }
    onSave({ registries: trimmed })
    onOpenChange(false)
  }

  const handleImportFile = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => {
      try {
        const parsed = JSON.parse(String(reader.result)) as unknown
        const regs = extractRegistriesFromUnknown(parsed)
        if (regs.length === 0) {
          throw new Error("no registries found")
        }
        setRegistries(regs)
        toast.success(t("pools.form.imagePullSecretImportSuccess", { count: regs.length }))
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        toast.error(t("pools.form.imagePullSecretImportError", { message }))
      }
    }
    reader.readAsText(file)
  }

  const handleExportJson = () => {
    const trimmed = registries
      .map((r) => ({
        registry: r.registry.trim(),
        username: r.username,
        password: r.password,
      }))
      .filter((r) => r.registry !== "" || r.username !== "" || r.password !== "")
    if (trimmed.length === 0) {
      toast.error(t("pools.form.imagePullSecretEmpty"))
      return
    }
    const payload = { registries: trimmed }
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = "image-pull-secret.json"
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("pools.form.imagePullSecretDialogTitle")}</DialogTitle>
        <DialogDescription>{t("pools.form.imagePullSecretDialogDesc")}</DialogDescription>
      </DialogHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto py-2">
        {/* Export / Import JSON */}
        <div className="flex flex-wrap items-center justify-end gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) handleImportFile(file)
              e.target.value = ""
            }}
          />
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={handleExportJson}
            className="font-mono text-[11px] tracking-wider uppercase"
          >
            <Download className="mr-1 h-3 w-3" />
            {t("pools.form.imagePullSecretExport")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => fileInputRef.current?.click()}
            className="font-mono text-[11px] tracking-wider uppercase"
          >
            <Upload className="mr-1 h-3 w-3" />
            {t("pools.form.imagePullSecretImport")}
          </Button>
        </div>

        {/* Registry list */}
        <div className="flex flex-col gap-3">
          {registries.map((reg, idx) => {
            const preset = PRESET_REGISTRIES.find((p) => p.value === reg.registry)
            const selectValue = preset ? preset.value : CUSTOM_SENTINEL
            const isCustom = !preset
            return (
              <div
                key={idx}
                className="border-border flex flex-col gap-2 rounded border px-3 py-2"
              >
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground font-mono text-[11px] font-semibold tracking-wide uppercase">
                    {t("pools.form.imagePullSecretRegistry")} #{idx + 1}
                  </span>
                  {registries.length > 1 && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => removeRegistry(idx)}
                      className="h-7 font-mono text-[11px]"
                    >
                      <Trash2 className="mr-1 h-3 w-3" />
                      {t("pools.form.imagePullSecretRemove")}
                    </Button>
                  )}
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <Field>
                    <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-wider uppercase">
                      {t("pools.form.imagePullSecretRegistry")}
                    </FieldLabel>
                    <Select
                      value={selectValue}
                      onValueChange={(val) => {
                        if (val === null || val === CUSTOM_SENTINEL) {
                          update(idx, { registry: "" })
                        } else {
                          update(idx, { registry: val })
                        }
                      }}
                    >
                      <SelectTrigger className="h-8 w-full font-mono text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {PRESET_REGISTRIES.map((p) => (
                          <SelectItem key={p.value} value={p.value}>
                            {p.label}
                          </SelectItem>
                        ))}
                        <SelectItem value={CUSTOM_SENTINEL}>
                          {t("pools.form.imagePullSecretRegistryCustom")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {isCustom && (
                      <Input
                        value={reg.registry}
                        onChange={(e) => update(idx, { registry: e.target.value })}
                        placeholder="registry.example.com"
                        className="mt-1 h-8 font-mono text-xs"
                      />
                    )}
                  </Field>
                  <Field>
                    <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-wider uppercase">
                      {t("pools.form.imagePullSecretUsername")}
                    </FieldLabel>
                    <Input
                      value={reg.username}
                      onChange={(e) => update(idx, { username: e.target.value })}
                      autoComplete="off"
                      className="h-8 font-mono text-xs"
                    />
                  </Field>
                </div>
                <Field>
                  <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-wider uppercase">
                    {t("pools.form.imagePullSecretPassword")}
                  </FieldLabel>
                  <div className="flex gap-1">
                    <Input
                      type={revealIndex === idx ? "text" : "password"}
                      value={reg.password}
                      onChange={(e) => update(idx, { password: e.target.value })}
                      autoComplete="new-password"
                      className="h-8 flex-1 font-mono text-xs"
                    />
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      onClick={() => setRevealIndex(revealIndex === idx ? null : idx)}
                      aria-label={t("pools.form.imagePullSecretTogglePassword")}
                      className="h-8 w-8"
                    >
                      {revealIndex === idx ? (
                        <EyeOff className="h-3.5 w-3.5" />
                      ) : (
                        <Eye className="h-3.5 w-3.5" />
                      )}
                    </Button>
                  </div>
                </Field>
              </div>
            )
          })}
        </div>

        <Button
          type="button"
          variant="outline"
          onClick={addRegistry}
          className="font-mono text-[11px] tracking-wider uppercase"
        >
          <Plus className="mr-1 h-3.5 w-3.5" />
          {t("pools.form.imagePullSecretAddRegistry")}
        </Button>
      </div>

      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={() => onOpenChange(false)}
          className="font-mono text-xs tracking-wider uppercase"
        >
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          onClick={handleSave}
          className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
        >
          {t("pools.form.imagePullSecretSave")}
        </Button>
      </DialogFooter>
    </>
  )
}

// extractRegistriesFromUnknown accepts either:
//   1. The ImagePullSecretExport shape: { registries: [...] }
//   2. A raw .dockerconfigjson: { auths: { "<registry>": { username, password, auth } } }
function extractRegistriesFromUnknown(parsed: unknown): RegistryCredential[] {
  if (!parsed || typeof parsed !== "object") {
    throw new Error("expected object")
  }
  const obj = parsed as Record<string, unknown>
  if (Array.isArray(obj.registries)) {
    return (obj.registries as Array<Record<string, unknown>>).map((r) => ({
      registry: String(r.registry ?? ""),
      username: String(r.username ?? ""),
      password: String(r.password ?? ""),
    }))
  }
  if (obj.auths && typeof obj.auths === "object") {
    const auths = obj.auths as Record<string, Record<string, unknown>>
    return Object.entries(auths).map(([registry, entry]) => {
      let username = typeof entry.username === "string" ? entry.username : ""
      let password = typeof entry.password === "string" ? entry.password : ""
      if ((!username || !password) && typeof entry.auth === "string") {
        try {
          const decoded = atob(entry.auth)
          const sep = decoded.indexOf(":")
          if (sep > 0) {
            if (!username) username = decoded.slice(0, sep)
            if (!password) password = decoded.slice(sep + 1)
          }
        } catch {
          /* ignore malformed base64 */
        }
      }
      return { registry, username, password }
    })
  }
  throw new Error("unrecognized JSON shape")
}
