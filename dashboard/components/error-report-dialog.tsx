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

import { useAtom, useAtomValue } from "jotai"
import { X, Copy, CheckCheck } from "lucide-react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import { authAtom, errorReportAtom } from "@/lib/atoms"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"

export function ErrorReportDialog() {
  const { t } = useTranslation()
  const auth = useAtomValue(authAtom)
  const clusterID = useClusterID()
  const [report, setReport] = useAtom(errorReportAtom)
  const [copied, setCopied] = useState(false)

  const close = () => {
    setReport(null)
  }

  const handleCopy = () => {
    if (!report) return
    const text = JSON.stringify(
      {
        context: { clusterID: clusterID, team: auth?.team, user: auth?.user },
        status: report.status,
        message: report.message,
        detail: report.detail,
        errorCode: report.errorCode,
      },
      null,
      2,
    )
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const detailText = report?.detail !== undefined ? JSON.stringify(report.detail, null, 2) : null

  return (
    <Dialog
      open={!!report}
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent className="border-border bg-card sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-destructive font-mono text-sm tracking-wide uppercase">
            {t("errors.errorDetails")}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            {report?.timestamp
              ? `${t("errors.occurredAt")} ${new Date(report.timestamp).toLocaleTimeString()}`
              : t("errors.anErrorOccurred")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex max-h-[60vh] flex-col gap-3 overflow-auto py-1">
          {/* Cluster + Team + User */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {clusterID && (
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("errors.cluster")}
                </div>
                <code className="text-foreground font-mono text-xs">{clusterID}</code>
              </div>
            )}
            {auth?.team && (
              <div className="border-border bg-secondary border px-3 py-2">
                <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                  {t("errors.team")}
                </div>
                <code className="text-foreground font-mono text-xs">{auth.team}</code>
              </div>
            )}
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("errors.user")}
              </div>
              <code className="text-foreground font-mono text-xs">{auth?.user ?? "Unknown"}</code>
            </div>
          </div>
          {/* Error message */}
          {report?.status && (
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("errors.statusCode")}
              </div>
              <code className="text-foreground font-mono text-xs">{report.status}</code>
            </div>
          )}
          <div className="border-border bg-secondary border px-3 py-2">
            <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
              {t("errors.message")}
            </div>
            <p className="text-foreground font-mono text-sm">{report?.message}</p>
          </div>

          {/* Error code badge */}
          {report?.errorCode && (
            <div className="border-border bg-secondary border px-3 py-2">
              <div className="text-muted-foreground mb-1 font-mono text-xs tracking-wider uppercase">
                {t("errors.errorCode")}
              </div>
              <code className="text-foreground font-mono text-xs">{report.errorCode}</code>
            </div>
          )}

          {/* Detail JSON */}
          {detailText && (
            <div className="border-border bg-secondary border">
              <div className="flex items-center justify-between px-3 py-1.5">
                <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
                  {t("errors.detail")}
                </span>
              </div>
              <pre className="max-h-48 overflow-auto px-3 pb-3 font-mono text-[11px] leading-relaxed break-all whitespace-pre-wrap">
                {detailText}
              </pre>
            </div>
          )}
        </div>

        <DialogFooter className="gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={close}
            className="font-mono text-xs tracking-wider uppercase"
          >
            <X className="mr-1.5 h-3.5 w-3.5" />
            {t("common.close")}
          </Button>
          <Button
            type="button"
            variant="default"
            onClick={handleCopy}
            className="font-mono text-xs tracking-wider uppercase"
          >
            {copied ? (
              <CheckCheck className="h-3 w-3 text-green-50" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
            {t("common.copy").toUpperCase()}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
