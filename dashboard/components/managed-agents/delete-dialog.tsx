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
import type { ManagedAgent } from "@/lib/api/managed-agent-types"
import { useDeleteManagedAgent } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  agent: ManagedAgent | null
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}

/**
 * Confirmation dialog for deleting a ManagedAgent. The Brain Deployment and a
 * derived (`hands.auto`) SandboxEnv are cascade-deleted through their owner
 * references; an env reached by `hands.envRef` is left untouched because the
 * controller never owned it.
 */
export function DeleteManagedAgentDialog({ agent, onOpenChange, onDeleted }: Props) {
  const { t } = useTranslation()
  const { mutate, isPending } = useDeleteManagedAgent()

  const derivedEnv = agent?.spec.hands?.auto ? (agent.status?.hands?.envName ?? "") : ""

  return (
    <Dialog open={!!agent} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("managedAgents.delete.title")}</DialogTitle>
          <DialogDescription>
            {derivedEnv
              ? t("managedAgents.delete.descriptionWithEnv", {
                  name: agent?.name ?? "",
                  env: derivedEnv,
                })
              : t("managedAgents.delete.description", { name: agent?.name ?? "" })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={!agent || isPending}
            onClick={() =>
              agent &&
              mutate(agent.name, {
                onSuccess: () => {
                  toast.success(t("managedAgents.delete.toast", { name: agent.name }))
                  onOpenChange(false)
                  onDeleted?.()
                },
              })
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
