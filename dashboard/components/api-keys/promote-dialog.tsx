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

import { Globe } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import type { AgentboxApiKey } from "@/lib/api/client"
import { usePromoteApiKey } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface PromoteApiKeyDialogProps {
  apiKey: AgentboxApiKey | null
  onOpenChange: (open: boolean) => void
}

export function PromoteApiKeyDialog({ apiKey, onOpenChange }: PromoteApiKeyDialogProps) {
  const { mutate: promoteKey, isPending } = usePromoteApiKey()
  const { t } = useTranslation()

  const handlePromote = () => {
    if (!apiKey) return
    promoteKey(
      { params: { path: { name: apiKey.keyId } } },
      {
        onSuccess: () => {
          toast.success(t("apiKeys.promoteSuccess"))
          onOpenChange(false)
        },
        onError: (err) => {
          const msg = err instanceof Error ? err.message : String(err)
          toast.error(msg)
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
          <DialogTitle className="font-mono text-sm tracking-wide uppercase">
            {t("apiKeys.promoteTitle")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {t("apiKeys.promoteDescription")}
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
              {apiKey.team && (
                <div className="border-border bg-secondary border px-3 py-2">
                  <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                    {t("apiKeys.col.team")}
                  </div>
                  <span className="font-mono text-xs">{apiKey.team}</span>
                </div>
              )}
              {apiKey.user && (
                <div className="border-border bg-secondary border px-3 py-2">
                  <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                    {t("apiKeys.col.user")}
                  </div>
                  <span className="font-mono text-xs">{apiKey.user}</span>
                </div>
              )}
            </div>
          </div>
        )}

        <DialogFooter className="gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {t("common.cancel")}
          </Button>
          <Button
            onClick={handlePromote}
            disabled={isPending}
            className="bg-foreground text-background hover:bg-foreground/90 gap-1.5 font-mono text-xs tracking-wider uppercase"
          >
            {isPending ? (
              <span className="flex items-center gap-1.5">
                <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
                {t("common.loading")}
              </span>
            ) : (
              <span className="flex items-center gap-1.5">
                <Globe className="h-3.5 w-3.5" />
                {t("apiKeys.promoteConfirm")}
              </span>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
