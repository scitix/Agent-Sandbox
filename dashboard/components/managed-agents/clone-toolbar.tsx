// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

"use client"

import { useRef } from "react"
import type { UseFormGetValues, UseFormTrigger } from "react-hook-form"
import { Download, Upload } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { clustersAtom } from "@/lib/atoms"
import { useAtomValue } from "jotai"
import { cloneFileName, fromCloneJson, toClonePayload } from "@/lib/utils/managed-agent-clone"
import type { FormValues } from "@/lib/utils/managed-agent-form"
import { downloadTextFile } from "@/lib/utils/download"

/**
 * Export / import of the form's own values, for cloning an agent.
 *
 * Export is not gated on validation: a clone file is a draft being moved between
 * agents, not something applied to a cluster, and an incomplete draft imports into
 * a form that shows exactly what is still missing. Import is create-only — an edit
 * replacing a live agent's whole configuration from a file is a different, riskier
 * operation than cloning, and nothing here asks for it.
 */
export function CloneToolbar({
  isEdit,
  getValues,
  trigger,
  onImport,
}: {
  isEdit: boolean
  getValues: UseFormGetValues<FormValues>
  trigger: UseFormTrigger<FormValues>
  onImport: (values: FormValues) => void
}) {
  const { t } = useTranslation()
  const clusters = useAtomValue(clustersAtom).clusters
  const fileInput = useRef<HTMLInputElement>(null)

  const handleExport = () => {
    const values = getValues()
    downloadTextFile(
      cloneFileName(values),
      JSON.stringify(toClonePayload(values), null, 2),
      "application/json",
    )
  }

  const handleFile = async (file: File) => {
    const parsed = fromCloneJson(await file.text(), {
      knownClusterIDs: clusters.map((c) => c.id),
    })
    if (!parsed.ok) {
      const message = {
        json: t("managedAgents.form.importInvalidJson"),
        kind: t("managedAgents.form.importWrongKind"),
        version: t("managedAgents.form.importVersionMismatch"),
        schema: t("managedAgents.form.importSchemaError", { detail: parsed.error.detail }),
      }[parsed.error.kind]
      toast.error(message)
      return
    }
    onImport(parsed.result.values)
    for (const warning of parsed.result.warnings) {
      if (warning.key === "secretsOmitted") {
        toast.success(t("managedAgents.form.importSuccess", { count: String(warning.count) }))
      } else {
        toast.warning(
          t("managedAgents.form.importUnknownClusters", { count: String(warning.count) }),
        )
      }
    }
    void trigger()
  }

  return (
    <div className="flex items-center justify-end gap-2 px-6 py-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 font-mono text-[11px] tracking-wider uppercase"
        onClick={handleExport}
        title={t("managedAgents.form.exportJsonDesc")}
      >
        <Download className="size-3.5" />
        {t("managedAgents.form.exportJson")}
      </Button>
      {!isEdit && (
        <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 font-mono text-[11px] tracking-wider uppercase"
            onClick={() => fileInput.current?.click()}
          >
            <Upload className="size-3.5" />
            {t("managedAgents.form.importJson")}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              // Reset first so re-picking the same file fires change again.
              e.target.value = ""
              if (file) void handleFile(file)
            }}
          />
        </>
      )}
    </div>
  )
}
