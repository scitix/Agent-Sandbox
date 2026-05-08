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
import type { AgentboxApiKey } from "@/lib/api/client"
import { toast } from "sonner"
import { useDeleteApiKey } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface RevokeApiKeyDialogProps {
  apiKey: AgentboxApiKey | null
  onOpenChange: (open: boolean) => void
  onRevoked?: () => void
}

export function RevokeApiKeyDialog({ apiKey, onOpenChange, onRevoked }: RevokeApiKeyDialogProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useDeleteApiKey()

  const handleRevoke = () => {
    if (!apiKey) return
    mutate(
      { params: { path: { name: apiKey.keyId } } },
      {
        onSuccess: () => {
          toast.success(t("apiKeys.revokedSuccessWithId", { keyId: apiKey.keyId }))
          onOpenChange(false)
          onRevoked?.()
        },
      },
    )
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
            <X className="mr-1.5 h-3.5 w-3.5" />
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={handleRevoke}
            disabled={isMutating}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {isMutating ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
            )}
            {t("common.revoke")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
