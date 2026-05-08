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
import { Eye, EyeOff, Copy, CheckCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { useTranslation } from "@/lib/i18n"

export interface KeyRevealResult {
  apiKey: string
  keyId: string
  role?: string
  team?: string
}

interface KeyRevealModalProps {
  result: KeyRevealResult
  onClose: () => void
}

export function KeyRevealModal({ result, onClose }: KeyRevealModalProps) {
  const { t } = useTranslation()
  const [showKey, setShowKey] = useState(false)
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    void navigator.clipboard.writeText(result.apiKey)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="border-border bg-card sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm tracking-wide text-green-600 uppercase dark:text-green-400">
            {t("apiKeys.apiKeyCreated")}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-2">
          <div className="border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
            <p className="font-mono text-xs text-amber-700 dark:text-amber-400">
              ⚠ {t("apiKeys.copyThisKeyNow")}
            </p>
          </div>
          <div className="border-border bg-secondary border px-3 py-2">
            <div className="flex items-center gap-2">
              <code className="text-foreground flex-1 font-mono text-xs break-all">
                {showKey ? result.apiKey : `${result.apiKey.slice(0, 12)}${"•".repeat(30)}`}
              </code>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground h-6 w-6 shrink-0"
                onClick={() => setShowKey(!showKey)}
              >
                {showKey ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground h-6 w-6 shrink-0"
                onClick={handleCopy}
              >
                {copied ? (
                  <CheckCheck className="h-3 w-3 text-green-500" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </Button>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2 font-mono text-xs">
            <div className="border-border bg-secondary border px-2 py-1.5">
              <div className="text-muted-foreground mb-0.5 text-xs tracking-wider uppercase">
                {t("apiKeys.col.keyId")}
              </div>
              <span>{result.keyId}</span>
            </div>
            {result.team && (
              <div className="border-border bg-secondary border px-2 py-1.5">
                <div className="text-muted-foreground mb-0.5 text-xs tracking-wider uppercase">
                  {t("apiKeys.col.team")}
                </div>
                <span>{result.team}</span>
              </div>
            )}
          </div>
          {result.role && (
            <div className="border-border bg-secondary border px-2 py-1.5 font-mono text-xs">
              <div className="text-muted-foreground mb-0.5 text-xs tracking-wider uppercase">
                {t("apiKeys.col.role")}
              </div>
              <span>{result.role}</span>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button
            onClick={onClose}
            className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
          >
            {t("common.done")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
