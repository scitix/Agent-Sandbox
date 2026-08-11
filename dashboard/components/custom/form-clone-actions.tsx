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
import type { FieldValues, UseFormGetValues } from "react-hook-form"
import { Download, Upload } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { downloadTextFile } from "@/lib/utils/download"
import type { FormClone } from "@/lib/utils/form-clone"

interface Props<T extends FieldValues> {
  /** The form's clone spec, from `createFormClone`. */
  clone: FormClone<T>
  getValues: UseFormGetValues<T>
  /** Defaults an imported file is merged onto — the ones in force right now. */
  defaults: T
  /** Hand the parsed values to the form, typically `reset(v)` then `trigger()`. */
  onImport: (values: T) => void
  /**
   * Import is create-only by default: replacing a live resource's whole
   * configuration from a file is a different, riskier operation than seeding a
   * new one, and no form asks for it.
   */
  canImport?: boolean
}

/**
 * Export / import buttons for any upsert form, meant for the left side of a
 * sheet footer.
 *
 * Export is not gated on validation — the file is a configuration being moved,
 * not something applied to a cluster, and an incomplete draft imports into a
 * form that shows exactly what is still missing. Secrets are blanked by the
 * spec's `stripSecrets`; see lib/utils/form-clone.ts.
 *
 * Every string here is shared (`common.formClone.*`) so adding this to another
 * form needs no new copy — pass a spec and the form's defaults.
 */
export function FormCloneActions<T extends FieldValues>({
  clone,
  getValues,
  defaults,
  onImport,
  canImport = true,
}: Props<T>) {
  const { t } = useTranslation()
  const fileInput = useRef<HTMLInputElement>(null)

  const handleExport = () => {
    const values = getValues()
    downloadTextFile(
      clone.fileName(values),
      JSON.stringify(clone.toPayload(values), null, 2),
      "application/json",
    )
  }

  const handleFile = async (file: File) => {
    const parsed = clone.fromJson(await file.text(), defaults)
    if (!parsed.ok) {
      toast.error(
        {
          json: t("common.formClone.invalidJson"),
          kind: t("common.formClone.wrongKind"),
          version: t("common.formClone.versionMismatch"),
          schema: t("common.formClone.schemaError", { detail: parsed.error.detail }),
        }[parsed.error.kind],
      )
      return
    }

    onImport(parsed.result.values)

    const secrets = parsed.result.warnings.find((w) => w.key === "secretsOmitted")
    const clusters = parsed.result.warnings.find((w) => w.key === "unknownClusters")
    if (secrets) {
      toast.warning(t("common.formClone.importedSecretsOmitted", { count: String(secrets.count) }))
    } else {
      toast.success(t("common.formClone.imported"))
    }
    if (clusters) {
      toast.warning(t("common.formClone.unknownClusters", { count: String(clusters.count) }))
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="text-muted-foreground h-8 gap-1.5 text-xs"
        onClick={handleExport}
        title={t("common.formClone.exportDesc")}
      >
        <Download className="h-3.5 w-3.5" />
        {t("common.formClone.export")}
      </Button>
      {canImport && (
        <>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="text-muted-foreground h-8 gap-1.5 text-xs"
            onClick={() => fileInput.current?.click()}
            title={t("common.formClone.importDesc")}
          >
            <Upload className="h-3.5 w-3.5" />
            {t("common.formClone.import")}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              // Cleared first so re-picking the same file fires onChange again.
              e.target.value = ""
              if (file) void handleFile(file)
            }}
          />
        </>
      )}
    </div>
  )
}
