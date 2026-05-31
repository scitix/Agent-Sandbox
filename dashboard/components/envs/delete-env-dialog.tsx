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

import { Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import type { AgentSandboxEnvSummary } from "@/lib/api/client"
import { useDeleteEnv } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  env: AgentSandboxEnvSummary | null
  onOpenChange: (open: boolean) => void
}

/**
 * Confirmation dialog for deleting a SandboxEnv. K8s garbage collection
 * cascade-deletes every member SandboxPool via the controlling OwnerRef
 * the Env Reconciler stamps on each Pool.
 */
export function DeleteEnvDialog({ env, onOpenChange }: Props) {
  const { t } = useTranslation()
  const { mutate, isPending } = useDeleteEnv()

  const memberCount = env?.memberCount ?? 0

  return (
    <Dialog open={!!env} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("envs.delete.title")}</DialogTitle>
          <DialogDescription>
            {t("envs.delete.description", { name: env?.name ?? "", count: memberCount })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={!env || isPending}
            onClick={() =>
              env &&
              mutate(
                { params: { path: { name: env.name } } },
                {
                  onSuccess: () => {
                    toast.success(t("envs.delete.toast", { name: env.name }))
                    onOpenChange(false)
                  },
                  onError: (err) => toast.error(err?.error ?? String(err)),
                },
              )
            }
            className="gap-1.5"
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t("common.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
