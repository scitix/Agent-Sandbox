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

import { Loader2, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import type { AgentSandboxTemplate, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { toast } from "sonner"
import { useDeleteTemplate } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface DeleteTemplateDialogProps {
  template: AgentSandboxTemplate | AgentSandboxTemplateSummary | null
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}

export function DeleteTemplateDialog({
  template,
  onOpenChange,
  onDeleted,
}: DeleteTemplateDialogProps) {
  const { mutate, isPending: isMutating } = useDeleteTemplate()
  const { t } = useTranslation()

  const handleDelete = () => {
    if (!template) return
    mutate(template.name, {
      onSuccess: () => {
        toast.success(t("templates.deletedSuccess", { name: template.name }))
        onOpenChange(false)
        onDeleted?.()
      },
    })
  }

  return (
    <Dialog
      open={!!template}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <DialogContent className="border-border bg-card sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-destructive font-mono text-sm tracking-wide uppercase">
            {t("templates.deleteTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("templates.deleteDescription")}
          </DialogDescription>
        </DialogHeader>

        {template && (
          <div className="py-2">
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("templates.form.name")}
              </div>
              <code className="text-foreground font-mono text-sm">{template.name}</code>
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
            <X className="mr-1.5 h-3.5 w-3.5" />
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={handleDelete}
            disabled={isMutating}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {isMutating ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
            )}
            {t("common.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
