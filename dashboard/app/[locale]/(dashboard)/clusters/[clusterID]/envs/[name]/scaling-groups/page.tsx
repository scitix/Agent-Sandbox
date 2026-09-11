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

import { use, useState } from "react"
import { useQuery } from "@tanstack/react-query"

import { AutoscalingSection } from "@/components/envs/env-detail-sections"
import { UpsertAutoscalingGroupSheet } from "@/components/envs/upsert-autoscaling-group-sheet"
import { DeleteAutoscalingGroupDialog } from "@/components/envs/delete-autoscaling-group-dialog"
import { envQueryOptions } from "@/lib/queries"
import type { AgentEnvAutoscalingGroup } from "@/lib/api/client"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/** Autoscaling tab — scaling-group rules table + edit/delete dialogs. */
export default function EnvAutoscalingPage({ params }: PageProps) {
  const { name } = use(params)
  const { data } = useQuery(envQueryOptions(name))
  const env = data?.env

  const [editTarget, setEditTarget] = useState<AgentEnvAutoscalingGroup | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AgentEnvAutoscalingGroup | null>(null)

  if (!env) return null

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <AutoscalingSection
        env={env}
        onEdit={(g) => setEditTarget(g)}
        onDelete={(g) => setDeleteTarget(g)}
        fixed
      />

      <UpsertAutoscalingGroupSheet
        env={env}
        group={editTarget}
        open={!!editTarget}
        onOpenChange={(open) => {
          if (!open) setEditTarget(null)
        }}
      />
      <DeleteAutoscalingGroupDialog
        envName={env.name}
        group={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
    </div>
  )
}
