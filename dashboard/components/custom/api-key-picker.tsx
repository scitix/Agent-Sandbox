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

// Which key the docs snippet below is filled in with.
//
// The document leaves `${AGBX_API_KEY}` standing, so the reader has to be told
// which of their keys goes there — and the two modes are not interchangeable
// even though a sandbox cannot tell them apart: an `agent` key's platform
// writes wait for a person, which is exactly wrong for a snippet someone is
// about to paste into their own shell. Both facts are on the item, so the
// choice is made with them in view rather than from memory.

import Link from "next/link"
import { KeyRound } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select"
import type { GlobalApiKeyItem } from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"

export interface ApiKeyPickerProps {
  /** Selectable keys, newest first. */
  keys: GlobalApiKeyItem[]
  /** The selected key id. */
  value?: string
  onChange: (keyId: string) => void
  /** The list is still loading — show the control, not the empty state. */
  loading?: boolean
  className?: string
}

/** What a key is called in a list: its description, or its id when it has none. */
function keyLabel(key: GlobalApiKeyItem): string {
  return key.description || key.keyId
}

export function ApiKeyPicker({ keys, value, onChange, loading, className }: ApiKeyPickerProps) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()

  // Nothing to choose from: the way out is to make one, not a disabled control
  // with no explanation. The document below still renders, placeholder and all.
  if (!loading && keys.length === 0) {
    return (
      <Button
        variant="outline"
        size="sm"
        className={cn("h-9 gap-1.5 text-xs", className)}
        render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}
      >
        <KeyRound className="h-3.5 w-3.5" />
        {t("docs.apiKey.create")}
      </Button>
    )
  }

  const selected = keys.find((k) => k.keyId === value)

  return (
    <Select value={value} onValueChange={(v) => v && onChange(v)}>
      <SelectTrigger
        size="sm"
        // `h-9` rather than the `sm` height: this sits beside the copy button,
        // and a row of controls that disagree about their height reads as a
        // layout accident.
        className={cn("h-9 max-w-72 gap-1.5 text-xs", className)}
        aria-label={t("docs.apiKey.label")}
      >
        <KeyRound className="text-muted-foreground h-3.5 w-3.5" />
        {/* The label is rendered here rather than through `SelectValue`: the
            items carry a mode badge and a role, and the trigger wants only the
            name of what is chosen. */}
        <span className="truncate font-mono">
          {selected ? keyLabel(selected) : t("docs.apiKey.choose")}
        </span>
      </SelectTrigger>
      <SelectContent align="end" className="max-w-[24rem]">
        {keys.map((key) => (
          <SelectItem key={key.keyId} value={key.keyId} className="items-start">
            <span className="flex min-w-0 flex-col gap-0.5 py-0.5 text-left">
              <span className="flex items-center gap-2">
                <span className="truncate font-mono text-xs">{keyLabel(key)}</span>
                <Badge variant={key.mode === "agent" ? "default" : "outline"} className="text-[10px]">
                  {t(key.mode === "agent" ? "apiKeys.mode.agent" : "apiKeys.mode.unrestricted")}
                </Badge>
              </span>
              <span className="text-muted-foreground font-mono text-[11px]">
                {[key.role, key.keyId].filter(Boolean).join(" · ")}
              </span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
