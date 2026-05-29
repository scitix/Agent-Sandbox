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

import type { LucideIcon } from "lucide-react"
import { CopyButton } from "@/components/custom/button/copy-button"

export interface DetailMetaItem {
  label: string
  value: React.ReactNode
}

interface DetailHeaderProps {
  /** Leading glyph identifying the resource kind. */
  icon?: LucideIcon
  /** Primary name/id of the resource. */
  title: string
  /** When set, renders an inline copy button next to the title. */
  copyValue?: string
  /** Small uppercase label under the title (e.g. the resource kind). */
  kind?: string
  /** Status badge or similar, shown inline with the title. */
  badge?: React.ReactNode
  /** Key/value chips describing the resource — the "basic info" row. */
  meta?: DetailMetaItem[]
  /** Right-aligned action buttons. */
  actions?: React.ReactNode
}

/**
 * Shared header for resource detail pages: a basic-info block (icon, name with
 * copy, status, key metadata) on top and right-aligned actions. Detail pages
 * render their tabs/sections below it for a consistent look across Sandbox,
 * Template, Dataset and Env.
 */
export function DetailHeader({
  icon: Icon,
  title,
  copyValue,
  kind,
  badge,
  meta,
  actions,
}: DetailHeaderProps) {
  return (
    <div className="shrink-0 px-6 py-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          {Icon && (
            <div className="bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
              <Icon className="h-4 w-4" />
            </div>
          )}
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate font-mono text-base font-semibold">{title}</h1>
              {copyValue && <CopyButton text={copyValue} />}
              {badge}
            </div>
            {kind && (
              <p className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
                {kind}
              </p>
            )}
          </div>
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>

      {meta && meta.length > 0 && (
        <div className="mt-4 flex flex-wrap gap-x-8 gap-y-3">
          {meta.map((m, i) => (
            <div key={i} className="flex flex-col gap-0.5">
              <span className="text-muted-foreground font-mono text-[10px] font-bold tracking-[0.12em] uppercase">
                {m.label}
              </span>
              <span className="text-foreground font-mono text-xs break-all">{m.value}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
