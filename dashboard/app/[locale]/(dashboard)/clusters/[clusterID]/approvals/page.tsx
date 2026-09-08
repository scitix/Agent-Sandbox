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

/**
 * Where a person answers what an agent is waiting on.
 *
 * This page is the destination of the link in every refusal, and it is reached
 * by people who did not go looking for it — an agent told them to come. So it
 * opens on the thing they were sent for: the `?id=` from the link is
 * highlighted and pulled to the top, and everything else is context.
 *
 * The decision goes out on the session JWT. That is not an implementation
 * detail: the server refuses a decision made with an API key, because a
 * credential that could approve its own writes would make the gate a
 * formality. This page is the one caller that authenticates as a person.
 */

import { useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "next/navigation"
import { useQuery } from "@tanstack/react-query"
import { Check, Clock, ShieldCheck, Trash2, X } from "lucide-react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { RelativeTime } from "@/components/custom/relative-time"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import {
  approvalsQueryOptions,
  useDecideApproval,
  useRevokeApprovalGrant,
  type ApprovalGrant,
  type ApprovalRequest,
  type ApprovalScope,
} from "@/lib/queries"

export default function ApprovalsPage() {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const params = useSearchParams()
  const focusId = params.get("id") ?? ""

  const { data, isLoading } = useQuery(approvalsQueryOptions(clusterID))
  const pending = useMemo(() => {
    const items = data?.pending ?? []
    // The one they were sent for goes first. Sorting rather than filtering:
    // an agent may be holding several, and hiding the rest would make the page
    // look empty the moment they answer the one they arrived for.
    return [...items].sort((a, b) =>
      a.id === focusId ? -1 : b.id === focusId ? 1 : 0
    )
  }, [data?.pending, focusId])
  const grants = data?.grants ?? []

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-6">
      <section className="flex flex-col gap-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[15px] font-medium">{t("approvals.pending.title")}</h2>
          <p className="text-muted-foreground text-[13px]">
            {t("approvals.pending.subtitle")}
          </p>
        </div>

        {isLoading ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-28 rounded-xl" />
            <Skeleton className="h-28 rounded-xl" />
          </div>
        ) : pending.length === 0 ? (
          <Empty className="bg-card rounded-xl border py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ShieldCheck />
              </EmptyMedia>
              <EmptyTitle>{t("approvals.pending.emptyTitle")}</EmptyTitle>
              <EmptyDescription>
                {t("approvals.pending.emptyBody")}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          pending.map(r => (
            <PendingCard
              key={r.id}
              request={r}
              focused={r.id === focusId}
              clusterID={clusterID}
            />
          ))
        )}
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[15px] font-medium">{t("approvals.granted.title")}</h2>
          <p className="text-muted-foreground text-[13px]">
            {t("approvals.granted.subtitle")}
          </p>
        </div>
        {grants.length === 0 ? (
          <p className="text-muted-foreground bg-card rounded-xl border p-4 text-[13px]">
            {t("approvals.granted.empty")}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {grants.map(g => (
              <GrantRow key={g.id} grant={g} clusterID={clusterID} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

function PendingCard({
  request,
  focused,
  clusterID,
}: {
  request: ApprovalRequest
  focused: boolean
  clusterID: string
}) {
  const { t } = useTranslation()
  const decide = useDecideApproval(clusterID)
  const ref = useRef<HTMLDivElement>(null)

  // Bring the card they were linked to into view. Someone arriving from a
  // refusal has been told "go here and approve this one"; making them find it
  // in a list is the difference between a link that works and one that starts
  // a search.
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ block: "center" })
  }, [focused])

  const act = (decision: "approve" | "deny", scope?: ApprovalScope) => {
    decide.mutate(
      { id: request.id, decision, scope },
      {
        onSuccess: () =>
          toast.success(
            decision === "approve"
              ? t("approvals.toast.approved")
              : t("approvals.toast.denied")
          ),
        onError: (e: unknown) =>
          toast.error(
            (e as { error?: string })?.error ?? t("approvals.toast.failed")
          ),
      }
    )
  }

  return (
    <div
      ref={ref}
      className={cn(
        "bg-card flex flex-col gap-3 rounded-xl border p-4",
        focused && "ring-primary/40 ring-2"
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[15px] font-medium">{request.summary}</span>
        <Badge variant="secondary" className="font-normal">
          {request.operation}
        </Badge>
        {request.onceOnly ? (
          <Badge variant="outline" className="font-normal">
            {t("approvals.badge.onceOnly")}
          </Badge>
        ) : null}
      </div>

      <div className="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-[13px]">
        <span className="font-mono">
          {request.method} {request.path}
        </span>
        <span>
          {t("approvals.asked", {
            who: `${request.principal.team}/${request.principal.user}`,
          })}
        </span>
        {request.sessionId ? (
          <span className="font-mono">{request.sessionId}</span>
        ) : (
          // Worth saying rather than leaving blank: it is why the "for this
          // session" button is missing on this card.
          <span>{t("approvals.noSession")}</span>
        )}
        <span className="flex items-center gap-1">
          <Clock className="size-3.5" />
          <RelativeTime date={request.expiresAt} />
        </span>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={decide.isPending} onClick={() => act("approve", "once")}>
          <Check className="size-4" />
          {t("approvals.action.once")}
        </Button>
        {/* The wider scopes are offered only where the server would accept
            them. Showing a button that always errors teaches people to ignore
            the ones that work. */}
        {!request.onceOnly && request.sessionId ? (
          <Button
            size="sm"
            variant="secondary"
            disabled={decide.isPending}
            onClick={() => act("approve", "session")}
          >
            {t("approvals.action.session")}
          </Button>
        ) : null}
        {!request.onceOnly && request.principal.keyId ? (
          <Button
            size="sm"
            variant="secondary"
            disabled={decide.isPending}
            onClick={() => act("approve", "key")}
          >
            {t("approvals.action.key")}
          </Button>
        ) : null}
        <Button
          size="sm"
          variant="ghost"
          disabled={decide.isPending}
          onClick={() => act("deny")}
        >
          <X className="size-4" />
          {t("approvals.action.deny")}
        </Button>
      </div>
    </div>
  )
}

function GrantRow({ grant, clusterID }: { grant: ApprovalGrant; clusterID: string }) {
  const { t } = useTranslation()
  const revoke = useRevokeApprovalGrant(clusterID)
  const [confirming, setConfirming] = useState(false)

  return (
    <div className="bg-card flex flex-wrap items-center gap-3 rounded-lg border px-4 py-2.5">
      <span className="text-[13px] font-medium">{grant.operation}</span>
      <Badge variant="secondary" className="font-normal">
        {grant.scope === "session"
          ? t("approvals.scope.session")
          : t("approvals.scope.key")}
      </Badge>
      <span className="text-muted-foreground font-mono text-xs">
        {grant.sessionId || grant.principal.keyId}
      </span>
      {grant.expiresAt ? (
        <span className="text-muted-foreground text-xs">
          <RelativeTime date={grant.expiresAt} />
        </span>
      ) : (
        // A key grant has no expiry, so this row is the only thing that will
        // ever end it.
        <span className="text-muted-foreground text-xs">
          {t("approvals.scope.untilRevoked")}
        </span>
      )}
      <Button
        size="sm"
        variant={confirming ? "destructive" : "ghost"}
        className="ml-auto"
        disabled={revoke.isPending}
        onClick={() => {
          if (!confirming) {
            setConfirming(true)
            return
          }
          revoke.mutate(grant.id, {
            onSuccess: () => toast.success(t("approvals.toast.revoked")),
            onError: () => toast.error(t("approvals.toast.failed")),
          })
        }}
      >
        <Trash2 className="size-4" />
        {confirming ? t("approvals.action.confirmRevoke") : t("approvals.action.revoke")}
      </Button>
    </div>
  )
}
