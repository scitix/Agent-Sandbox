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

import { useMemo, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Check, Loader2, Network } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { createEnvImperative } from "@/lib/queries"
import { useEnvNameAcrossClusters, type EnvPresence } from "@/hooks/use-env-name-across-clusters"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { AgentCreateSandboxEnvRequest, AgentSandboxEnv } from "@/lib/api/client"

interface Props {
  env: AgentSandboxEnv | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * A row's standing, which decides whether it can be selected.
 *
 * An environment is identified across clusters by name + template, and nothing
 * more: replica counts, scaling groups and other per-cluster tuning are expected
 * to differ, so a config difference is not a conflict. A *template* difference
 * is — that is a different environment wearing the same name, and creating over
 * it is not something this dialog should offer.
 */
type RowState = "extendable" | "extended" | "conflict" | "failed" | "loading"

interface Row {
  clusterID: string
  clusterName: string
  state: RowState
  /** Template bound on that cluster, for the conflict explanation. */
  templateName?: string
}

function classify(presence: EnvPresence, templateName: string | undefined): Row {
  const base = { clusterID: presence.clusterID, clusterName: presence.clusterName }
  switch (presence.state) {
    case "loading":
      return { ...base, state: "loading" }
    case "failed":
      return { ...base, state: "failed" }
    case "absent":
      return { ...base, state: "extendable" }
    case "present": {
      const theirs = presence.env.templateName
      return theirs === templateName
        ? { ...base, state: "extended", templateName: theirs }
        : { ...base, state: "conflict", templateName: theirs }
    }
  }
}

export function ExtendEnvDialog({ env, open, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {open && env && <ExtendEnvBody env={env} onClose={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function ExtendEnvBody({ env, onClose }: { env: AgentSandboxEnv; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const templateName = env.spec.templateRef?.name

  const presence = useEnvNameAcrossClusters(env.name)
  const rows = useMemo(
    () => presence.others.map((p) => classify(p, templateName)),
    [presence.others, templateName],
  )

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [submitting, setSubmitting] = useState(false)

  const extendable = rows.filter((r) => r.state === "extendable")
  const chosen = extendable.filter((r) => selected.has(r.clusterID))

  const toggle = (clusterID: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(clusterID)) next.add(clusterID)
      return next
    })

  const submit = async () => {
    // Env shell only — no member Pools. Pool sizing is bound to instance types
    // and quota, neither of which carries across clusters, so a copied member
    // would mostly fail to admit. The user adds Pools on the target cluster.
    const body: AgentCreateSandboxEnvRequest = {
      name: env.name,
      templateRef: env.spec.templateRef,
      // The spec leaves mode optional (the server defaults it); the create
      // request does not, so carry the source's mode or its default.
      mode: env.spec.mode ?? "WarmPool",
      ...(env.spec.overrides ? { overrides: env.spec.overrides } : {}),
    }

    setSubmitting(true)
    const results = await Promise.allSettled(
      chosen.map((r) => createEnvImperative(body, r.clusterID)),
    )
    setSubmitting(false)

    const failed = results.filter((r) => r.status === "rejected").length
    const succeeded = results.length - failed

    if (succeeded > 0) {
      toast.success(t("envs.extend.toast.created", { count: String(succeeded) }))
      // The probe reads each cluster's Env list; drop them so reopening the
      // dialog shows the new members rather than the pre-create snapshot.
      void qc.invalidateQueries({ queryKey: ["get", "/envs"] })
    }
    if (failed > 0) {
      toast.error(t("envs.extend.toast.failed", { count: String(failed) }))
    }
    if (failed === 0) onClose()
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("envs.extend.title")}</DialogTitle>
        <DialogDescription>
          {t("envs.extend.description", { name: env.name, template: templateName ?? "—" })}
        </DialogDescription>
      </DialogHeader>

      {rows.length === 0 ? (
        <p className="text-muted-foreground py-4 text-sm">{t("envs.extend.noOtherClusters")}</p>
      ) : (
        <div className="max-h-72 space-y-2 overflow-y-auto">
          {rows.map((row) => (
            <ClusterRow
              key={row.clusterID}
              row={row}
              checked={selected.has(row.clusterID)}
              onToggle={() => toggle(row.clusterID)}
            />
          ))}
        </div>
      )}

      {chosen.length > 0 && (
        <p className="text-muted-foreground rounded-md border border-dashed p-3 text-xs">
          {t("envs.extend.poolsNote")}
        </p>
      )}

      <DialogFooter>
        <Button variant="ghost" onClick={onClose} disabled={submitting}>
          {t("common.cancel")}
        </Button>
        <Button disabled={chosen.length === 0 || submitting} onClick={submit} className="gap-1.5">
          {submitting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Network className="h-3.5 w-3.5" />
          )}
          {t("envs.extend.confirm", { count: String(chosen.length) })}
        </Button>
      </DialogFooter>
    </>
  )
}

function ClusterRow({
  row,
  checked,
  onToggle,
}: {
  row: Row
  checked: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const selectable = row.state === "extendable"

  return (
    <label
      className={cn(
        "flex items-center gap-3 rounded-md border p-3",
        selectable ? "hover:bg-accent cursor-pointer" : "opacity-70",
      )}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={onToggle}
        disabled={!selectable}
        aria-label={row.clusterName}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{row.clusterName}</p>
        <p className="text-muted-foreground truncate font-mono text-xs">{row.clusterID}</p>
      </div>
      {row.state === "loading" && (
        <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
      )}
      {row.state === "extended" && (
        <span className="flex shrink-0 items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400">
          <Check className="h-3.5 w-3.5" />
          {t("envs.extend.state.extended")}
        </span>
      )}
      {row.state === "conflict" && (
        <span
          className="flex shrink-0 items-center gap-1 text-xs text-amber-600 dark:text-amber-400"
          title={t("envs.extend.state.conflictDetail", { template: row.templateName ?? "—" })}
        >
          <AlertTriangle className="h-3.5 w-3.5" />
          {t("envs.extend.state.conflict")}
        </span>
      )}
      {row.state === "failed" && (
        <span className="text-muted-foreground shrink-0 text-xs">
          {t("envs.extend.state.failed")}
        </span>
      )}
    </label>
  )
}
