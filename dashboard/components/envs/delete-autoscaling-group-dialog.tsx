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
import type { AgentEnvAutoscalingGroup } from "@/lib/api/client"
import { useDeleteEnvAutoscalingGroup } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  envName: string
  group: AgentEnvAutoscalingGroup | null
  onOpenChange: (open: boolean) => void
}

export function DeleteAutoscalingGroupDialog({ envName, group, onOpenChange }: Props) {
  const { t } = useTranslation()
  const { mutate, isPending } = useDeleteEnvAutoscalingGroup(envName)

  return (
    <Dialog open={!!group} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("envs.autoscaling.delete.title")}</DialogTitle>
          <DialogDescription>
            {t("envs.autoscaling.delete.description", { group: group?.name ?? "" })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={!group || isPending}
            onClick={() =>
              group &&
              mutate(
                { params: { path: { name: envName, groupName: group.name } } },
                {
                  onSuccess: () => {
                    toast.success(
                      t("envs.upsertAutoscaling.deletedToast", { group: group.name }),
                    )
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
