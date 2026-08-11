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

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { envQueryOptions } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { AgentSandboxEnv } from "@/lib/api/client"
import type { EnvPresence } from "@/hooks/use-env-name-across-clusters"

/** A cluster that already holds an Env of the name being typed. */
type Candidate = Extract<EnvPresence, { state: "present" }>

interface Props {
  /** The name that matched elsewhere. Null keeps the dialog closed. */
  name: string | null
  candidates: EnvPresence[]
  onClose: () => void
  onCopy: (source: AgentSandboxEnv) => void
}

/**
 * Offered when the name being typed already exists on another cluster.
 *
 * Same name + same template is how a cross-cluster environment is identified, so
 * a match here almost always means the user is extending an existing environment
 * rather than colliding with an unrelated one — and copying its configuration is
 * the shortcut worth offering. Per-cluster differences stay editable afterwards;
 * nothing is locked to the source.
 */
export function CopyEnvDialog({ name, candidates, onClose, onCopy }: Props) {
  const present = candidates.filter((c): c is Candidate => c.state === "present")
  const open = !!name && present.length > 0

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {open && <CopyEnvBody name={name} candidates={present} onClose={onClose} onCopy={onCopy} />}
      </DialogContent>
    </Dialog>
  )
}

function CopyEnvBody({
  name,
  candidates,
  onClose,
  onCopy,
}: {
  name: string
  candidates: Candidate[]
  onClose: () => void
  onCopy: (source: AgentSandboxEnv) => void
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<string>(candidates[0]?.clusterID ?? "")

  // Full spec of the chosen source — the list summary carries only the template
  // name, and the copy needs the overrides too.
  const { data, isLoading } = useQuery({
    ...envQueryOptions(name, selected),
    enabled: !!selected,
  })
  const source = data?.env

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("envs.form.copyFrom.title")}</DialogTitle>
        <DialogDescription>
          {t("envs.form.copyFrom.description", { name, count: String(candidates.length) })}
        </DialogDescription>
      </DialogHeader>

      <div className="max-h-64 space-y-2 overflow-y-auto">
        {candidates.map((c) => (
          <button
            key={c.clusterID}
            type="button"
            onClick={() => setSelected(c.clusterID)}
            aria-pressed={selected === c.clusterID}
            className={cn(
              "hover:bg-accent flex w-full items-center justify-between gap-3 rounded-md border p-3 text-left transition-colors",
              selected === c.clusterID && "border-brand bg-accent/50",
            )}
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{c.clusterName}</p>
              <p className="text-muted-foreground truncate font-mono text-xs">
                {c.env.templateName ?? "—"}
              </p>
            </div>
            <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
              {t("envs.col.members")}: {c.env.memberCount ?? 0}
            </span>
          </button>
        ))}
      </div>

      <p className="text-muted-foreground rounded-md border border-dashed p-3 text-xs">
        {t("envs.form.copyFrom.secretsNote")}
      </p>

      <DialogFooter>
        <DialogClose
          render={
            <Button variant="ghost" onClick={onClose}>
              {t("envs.form.copyFrom.startBlank")}
            </Button>
          }
        />
        <Button
          disabled={!source || isLoading}
          className="gap-1.5"
          onClick={() => {
            if (!source) return
            onCopy(source)
          }}
        >
          {isLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {t("envs.form.copyFrom.confirm")}
        </Button>
      </DialogFooter>
    </>
  )
}
