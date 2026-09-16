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
  Loader2,
  Copy,
  Check,
} from "lucide-react"
import { useState } from "react"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { ApiKeyPicker } from "@/components/custom/api-key-picker"
import type { AgentSandboxTemplate, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { clusterTemplateQueryOptions, templateQueryOptions } from "@/lib/queries/template"
import { useTranslation } from "@/lib/i18n"
import { useDocsApiKey } from "@/hooks/use-docs-api-key"

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
      className={`text-muted-foreground hover:text-foreground rounded-md p-0.5 transition-colors ${copied ? "text-green-500 hover:text-green-500" : ""}`}
      title={copied ? "Copied!" : "Copy name"}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  )
}

// ─── Inner content (only mounted when sheet is open → triggers fetch) ──────────

export function TemplateDetailContent({
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
  // The hub first, because that is the copy the edit form must round-trip.
  const hub = useQuery(templateQueryOptions(templateName))
  // A template applied straight to this cluster is absent from the hub. It is
  // still real and still listed, so read it from the cluster rather than
  // reporting it missing — with editing withheld, since the write goes to a hub
  // that has nothing to update.
  const local = useQuery({
    ...clusterTemplateQueryOptions(templateName),
    enabled: !!templateName && !hub.isLoading && !hub.data,
  })
  const envelope = hub.data ?? local.data
  const clusterOnly = !hub.data && !!local.data
  const isLoading = hub.isLoading || local.isLoading

  // The docs are rendered with the reader's own key — the template's copy of
  // them leaves `${AGBX_API_KEY}` standing, exactly as the Env page does, and
  // the picker beside the copy button decides what goes in its place. Called
  // before the early returns below, because a hook cannot sit behind one.
  const { keys, selected, select, fill, loading } = useDocsApiKey()

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
  const docs = tmpl.docs ? fill(tmpl.docs) : ""

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
        {/* The version badge leads the row: it is the one fact about a template
            that changes what the page below means, and the row is where the
            eye already is. CPU and memory used to sit in cards of their own —
            a template no longer pins a resource shape, so they said nothing
            about what a sandbox from this template would get. */}
        {tmpl.version && (
          <Badge variant="outline" className="h-9 gap-1 px-3 font-mono text-xs">
            <Layers className="h-3.5 w-3.5" />
            {tmpl.version}
          </Badge>
        )}
        {tmpl.docs && (
          <>
            <ApiKeyPicker
              keys={keys}
              value={selected?.keyId}
              onChange={select}
              loading={loading}
              className="ms-auto"
            />
            <CopyButton content={docs} label={t("common.copyPage")} />
          </>
        )}
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
        {isAdmin && onEdit && !clusterOnly && (
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

      {/* Documentation (main content area) */}
      {docs ? (
        <MarkdownRenderer content={docs} />
      ) : (
        <p className="text-muted-foreground text-sm">{t("templates.detail.noDocs")}</p>
      )}

      {/* CRD YAML — admin only */}
      {isAdmin && tmpl.crdYaml && (
        <section className="mt-6">
          <h3 className="text-muted-foreground mb-2 font-mono text-xs font-medium tracking-wider uppercase">
            {t("templates.detail.crdYaml")}
          </h3>
          <pre className="bg-secondary overflow-auto rounded-lg border p-3 font-mono text-xs leading-relaxed">
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
