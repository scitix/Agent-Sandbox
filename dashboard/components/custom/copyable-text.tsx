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

import { CheckCheck, Copy } from "lucide-react"
import { useState } from "react"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"

interface CopyableTextProps {
  value: string
  label?: string
  className?: string
}

export function CopyableText({ value, label, className }: CopyableTextProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const displayText = label ?? value

  const handleCopy = () => {
    navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <button
      onClick={handleCopy}
      className={cn(
        "group text-foreground hover:text-brand flex items-center gap-1 font-mono text-xs font-semibold transition-colors",
        className,
      )}
      title={`${t("common.copy")}: ${value}`}
    >
      <span>{displayText}</span>
      {copied ? (
        <CheckCheck className="h-3 w-3 shrink-0 text-green-500" />
      ) : (
        <Copy className="text-muted-foreground group-hover:text-brand h-3 w-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
      )}
    </button>
  )
}
