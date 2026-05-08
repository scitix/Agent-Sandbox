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
import type { AgentSandboxPool } from "@/lib/api/client"
import { toast } from "sonner"
import { useDeletePool } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface DeletePoolDialogProps {
  pool: AgentSandboxPool | null
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}

export function DeletePoolDialog({ pool, onOpenChange, onDeleted }: DeletePoolDialogProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useDeletePool()

  const handleDelete = () => {
    if (!pool) return
    mutate(
      { params: { path: { name: pool.name } } },
      {
        onSuccess: () => {
          toast.success(t("pools.deletedSuccess", { name: pool.name }))
          onOpenChange(false)
          onDeleted?.()
        },
      },
    )
  }

  return (
    <Dialog
      open={!!pool}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <DialogContent className="border-border bg-card sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-destructive font-mono text-sm tracking-wide uppercase">
            {t("pools.deleteTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("pools.deleteDescription")}
          </DialogDescription>
        </DialogHeader>

        {pool && (
          <div className="py-2">
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("pools.col.name")}
              </div>
              <code className="text-foreground font-mono text-sm">{pool.name}</code>
            </div>
            <div className="mt-2 grid grid-cols-3 gap-2">
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("pools.col.replicas")}
                </div>
                <span className="font-mono text-xs">{pool.spec?.replicas}</span>
              </div>
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("pools.col.running")}
                </div>
                <span className="font-mono text-xs">{pool.status?.runningReplicas ?? 0}</span>
              </div>
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("pools.col.namespace")}
                </div>
                <span className="block truncate font-mono text-xs">{pool.namespace}</span>
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
