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

import * as React from "react"
import Link from "next/link"
import { CheckCheck, Copy } from "lucide-react"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"

interface ResourceLinkProps {
  /** Full value used for the clipboard copy + hover title (e.g. the full id). */
  value: string
  /** Display text; falls back to `value` (used to show a shortened label). */
  label?: React.ReactNode
  /** Navigation target. Takes precedence over `onNavigate` when both are set. */
  href?: string
  /** Click handler used when there is no `href` (e.g. opens a detail sheet). */
  onNavigate?: () => void
  /** Hide the copy affordance entirely. */
  copyable?: boolean
  /** Text emphasis. `muted` matches secondary reference columns. */
  tone?: "default" | "muted"
  className?: string
}

/**
 * A table-cell label that reads as plain text at rest and, on hover, reveals
 * an underline on the name plus a compact copy button. Clicking the name
 * navigates to the resource's detail view (`href` or `onNavigate`); clicking
 * the copy button copies the full `value` without triggering navigation.
 *
 * Shared by the Sandbox id, Env name, Pool name and Template name columns.
 */
export function ResourceLink({
  value,
  label,
  href,
  onNavigate,
  copyable = true,
  tone = "default",
  className,
}: ResourceLinkProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = React.useState(false)

  const display = label ?? value
  const navigable = Boolean(href || onNavigate)

  const handleCopy = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const nameClass = cn(
    "font-mono text-xs underline-offset-2 transition-colors",
    navigable && "cursor-pointer group-hover/reslink:underline",
    tone === "muted"
      ? "text-muted-foreground hover:text-foreground"
      : "text-foreground hover:text-brand",
  )

  let nameEl: React.ReactNode
  if (href) {
    nameEl = (
      <Link href={href} className={nameClass} title={value}>
        {display}
      </Link>
    )
  } else if (onNavigate) {
    nameEl = (
      <button
        type="button"
        onClick={onNavigate}
        className={cn(nameClass, "bg-transparent p-0 text-left")}
        title={value}
      >
        {display}
      </button>
    )
  } else {
    nameEl = (
      <span className={nameClass} title={value}>
        {display}
      </span>
    )
  }

  return (
    <span className={cn("group/reslink inline-flex max-w-full items-center gap-1", className)}>
      {nameEl}
      {copyable && (
        <button
          type="button"
          onClick={handleCopy}
          title={`${t("common.copy")}: ${value}`}
          aria-label={t("common.copy")}
          className="text-muted-foreground hover:text-brand inline-flex h-4 w-4 shrink-0 items-center justify-center opacity-0 transition-opacity group-hover/reslink:opacity-100 focus-visible:opacity-100"
        >
          {copied ? (
            <CheckCheck className="h-3 w-3 text-green-500" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
        </button>
      )}
    </span>
  )
}
