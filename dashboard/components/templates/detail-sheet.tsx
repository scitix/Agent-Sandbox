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

import { useQuery } from "@tanstack/react-query"
import { parseAsString, useQueryState } from "nuqs"
import {
  Layers,
  Pencil,
  Trash2,
  Download,
  Cpu,
  MemoryStick,
  Loader2,
  Copy,
  Check,
} from "lucide-react"
import { useState } from "react"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import type { AgentSandboxTemplate, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { templateQueryOptions } from "@/lib/queries/template"
import { useTranslation } from "@/lib/i18n"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"

// ─── nuqs URL param name ───────────────────────────────────────────────────────

export const TEMPLATE_DETAIL_PARAM = "template"

// ─── Copy button ───────────────────────────────────────────────────────────────

function CopyButton({ content, label }: { content: string; label: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" onClick={handleCopy}>
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      {copied ? t("common.copied") : label}
    </Button>
  )
}

// ─── Inline copy button (header name) ─────────────────────────────────────────

function InlineCopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button
      onClick={handleCopy}
      className={`text-muted-foreground hover:text-foreground rounded p-0.5 transition-colors${copied ? " text-green-500 hover:text-green-500" : ""}`}
      title={copied ? "Copied!" : "Copy name"}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  )
}

// ─── Inner content (only mounted when sheet is open → triggers fetch) ──────────

function TemplateDetailContent({
  templateName,
  onEdit,
  onDelete,
  isAdmin,
}: {
  templateName: string
  onEdit?: (template: AgentSandboxTemplate) => void
  onDelete?: (template: AgentSandboxTemplateSummary) => void
  isAdmin: boolean
}) {
  const { t } = useTranslation()
  const { data: envelope, isLoading } = useQuery(templateQueryOptions(templateName))

  if (isLoading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
      </div>
    )
  }

  if (!envelope) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-muted-foreground text-sm">Template not found</p>
      </div>
    )
  }

  const tmpl = envelope.template
  const cpuCores = parseCpuToCore(tmpl.cpu)
  const memoryMiB = parseMemoryToMiB(tmpl.memory)

  const handleExportYaml = () => {
    if (!tmpl.crdYaml) return
    const blob = new Blob([tmpl.crdYaml], { type: "text/yaml" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `${tmpl.name}.yaml`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex-1 overflow-y-auto px-5 py-4">
      {/* Action buttons at top of content */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        {tmpl.docs && <CopyButton content={tmpl.docs} label={t("common.copyPage")} />}
        {isAdmin && tmpl.crdYaml && (
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 text-xs"
            onClick={handleExportYaml}
          >
            <Download className="h-3 w-3" />
            {t("templates.detail.exportYaml")}
          </Button>
        )}
        {isAdmin && onEdit && (
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 text-xs"
            onClick={() => onEdit(tmpl)}
          >
            <Pencil className="h-3 w-3" />
            {t("common.edit")}
          </Button>
        )}
        {isAdmin && onDelete && tmpl.syncSource === "global" && (
          <Button
            variant="outline"
            size="sm"
            className="text-destructive hover:text-destructive h-7 gap-1.5 text-xs"
            onClick={() => onDelete({ name: tmpl.name, syncSource: tmpl.syncSource })}
          >
            <Trash2 className="h-3 w-3" />
            {t("common.delete")}
          </Button>
        )}
      </div>

      {/* Resource cards */}
      <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {cpuCores != null && (
          <div className="border-border bg-secondary/30 flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
            <Cpu className="text-muted-foreground h-4 w-4 shrink-0" />
            <div>
              <p className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
                CPU
              </p>
              <p className="font-mono text-sm font-semibold">{formatCores(cpuCores)} cores</p>
            </div>
          </div>
        )}
        {memoryMiB != null && (
          <div className="border-border bg-secondary/30 flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
            <MemoryStick className="text-muted-foreground h-4 w-4 shrink-0" />
            <div>
              <p className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
                Memory
              </p>
              <p className="font-mono text-sm font-semibold">{formatMiB(memoryMiB)} MiB</p>
            </div>
          </div>
        )}
        {tmpl.version && (
          <div className="border-border bg-secondary/30 flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
            <Layers className="text-muted-foreground h-4 w-4 shrink-0" />
            <div>
              <p className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
                {t("templates.col.version")}
              </p>
              <p className="font-mono text-sm font-semibold">{tmpl.version}</p>
            </div>
          </div>
        )}
      </div>

      {/* Documentation (main content area) */}
      {tmpl.docs ? (
        <MarkdownRenderer content={tmpl.docs} />
      ) : (
        <p className="text-muted-foreground text-sm">{t("templates.detail.noDocs")}</p>
      )}

      {/* CRD YAML — admin only */}
      {isAdmin && tmpl.crdYaml && (
        <section className="mt-6">
          <h3 className="text-muted-foreground mb-2 font-mono text-xs font-medium tracking-wider uppercase">
            {t("templates.detail.crdYaml")}
          </h3>
          <pre className="bg-secondary overflow-auto rounded border p-3 font-mono text-xs leading-relaxed">
            {tmpl.crdYaml}
          </pre>
        </section>
      )}
    </div>
  )
}

// ─── Exported Sheet component ──────────────────────────────────────────────────

export function TemplateDetailSheet({
  onEdit,
  onDelete,
  isAdmin,
}: {
  onEdit?: (template: AgentSandboxTemplate) => void
  onDelete?: (template: AgentSandboxTemplateSummary) => void
  isAdmin: boolean
}) {
  const { t } = useTranslation()

  const [templateName, setTemplateName] = useQueryState(
    TEMPLATE_DETAIL_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )

  const isOpen = !!templateName

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      void setTemplateName(null)
    }
  }

  return (
    <Sheet open={isOpen} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-6xl"
      >
        <SheetHeader className="border-border border-b px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg">
              <Layers className="h-4 w-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <SheetTitle className="text-base font-semibold">{templateName}</SheetTitle>
                {templateName && <InlineCopyButton value={templateName} />}
              </div>
              <p className="text-muted-foreground mt-0.5 font-mono text-xs">
                {t("templates.detail.title")}
              </p>
            </div>
          </div>
        </SheetHeader>

        {isOpen && templateName && (
          <TemplateDetailContent
            templateName={templateName}
            onEdit={(tmpl) => {
              handleOpenChange(false)
              onEdit?.(tmpl)
            }}
            onDelete={(tmpl) => {
              handleOpenChange(false)
              onDelete?.(tmpl)
            }}
            isAdmin={isAdmin}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}
