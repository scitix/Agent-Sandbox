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

import { useRef } from "react"
import type { UseFormGetValues, UseFormTrigger } from "react-hook-form"
import { Download, Upload } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { downloadTextFile } from "@/lib/utils/download"
import { envCloneFileName, fromEnvCloneJson, toEnvClonePayload } from "@/lib/utils/env-clone"
import type { FormValues } from "@/lib/utils/env-form"

/**
 * Export / import of the Env form's own values.
 *
 * Export is not gated on validation: the file is a configuration being moved,
 * not something applied to a cluster, and an incomplete draft imports into a
 * form that shows exactly what is still missing. Credentials are blanked by
 * `toEnvClonePayload` — see lib/utils/env-clone.ts.
 *
 * Import is create-only. Replacing a live Env's whole configuration from a file
 * is a different, riskier operation than seeding a new one, and nothing here
 * asks for it.
 */
export function EnvCloneToolbar({
  getValues,
  trigger,
  onImport,
}: {
  getValues: UseFormGetValues<FormValues>
  trigger: UseFormTrigger<FormValues>
  onImport: (values: FormValues) => void
}) {
  const { t } = useTranslation()
  const fileInput = useRef<HTMLInputElement>(null)

  const handleExport = () => {
    const values = getValues()
    downloadTextFile(
      envCloneFileName(values),
      JSON.stringify(toEnvClonePayload(values), null, 2),
      "application/json",
    )
  }

  const handleFile = async (file: File) => {
    const parsed = fromEnvCloneJson(await file.text())
    if (!parsed.ok) {
      const message = {
        json: t("envs.form.importInvalidJson"),
        kind: t("envs.form.importWrongKind"),
        version: t("envs.form.importVersionMismatch"),
        schema: t("envs.form.importSchemaError", { detail: parsed.error.detail }),
      }[parsed.error.kind]
      toast.error(message)
      return
    }

    onImport(parsed.result.values)
    const omitted = parsed.result.warnings.find((w) => w.key === "secretsOmitted")
    if (omitted) {
      toast.warning(t("envs.form.importSecretsOmitted", { count: String(omitted.count) }))
    } else {
      toast.success(t("envs.form.importSuccess"))
    }
    void trigger()
  }

  return (
    <div className="flex items-center justify-end gap-2 border-b px-6 py-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 font-mono text-[11px] tracking-wider uppercase"
        onClick={handleExport}
        title={t("envs.form.exportJsonDesc")}
      >
        <Download className="size-3.5" />
        {t("envs.form.exportJson")}
      </Button>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 font-mono text-[11px] tracking-wider uppercase"
        onClick={() => fileInput.current?.click()}
      >
        <Upload className="size-3.5" />
        {t("envs.form.importJson")}
      </Button>
      <input
        ref={fileInput}
        type="file"
        accept=".json,application/json"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0]
          // Reset first so re-picking the same file fires onChange again.
          e.target.value = ""
          if (file) void handleFile(file)
        }}
      />
    </div>
  )
}
