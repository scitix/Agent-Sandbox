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

// The writes an agent is holding, waiting on a person.
//
// These go out on the SESSION JWT, not on an API key, and that is the whole
// point of the feature rather than an implementation detail: the server refuses
// a decision made with anything else, because a credential that could approve
// its own writes would make the gate a formality. The console is the one caller
// that authenticates as a person.

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { apiFor, fetchFor, delayedInvalidate } from "./utils"
import type { components } from "@/lib/api/schema"

export type ApprovalRequest = components["schemas"]["ApprovalRequest"]
export type ApprovalGrant = components["schemas"]["ApprovalGrant"]

/** How often the board re-reads while it is open.
 *
 *  A refusal is made by an agent that is waiting on this page, so the list has
 *  to grow without a reload — someone watching it should see the request appear
 *  rather than learn to hit refresh. Five seconds is well inside the fifteen
 *  minutes a request stays answerable. */
const POLL_MS = 5000

export const approvalsQueryOptions = (clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/approvals", undefined, {
    refetchInterval: POLL_MS,
  })

/** How far one decision reaches. `once` is always offered; the wider two are
 *  refused by the server for destructive and credential-minting calls, which is
 *  why the card reads `onceOnly` off the request rather than deciding here. */
export type ApprovalScope = "once" | "session" | "key"

export function useDecideApproval(clusterID?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      id: string
      decision: "approve" | "deny"
      scope?: ApprovalScope
    }) => {
      const { data, error } = await fetchFor(clusterID).POST(
        "/approvals/{approvalId}/decision",
        {
          params: { path: { approvalId: input.id } },
          // The server defaults an absent scope to the narrowest one, but the
          // generated body type requires it — send it explicitly rather than
          // relying on a default the type system cannot see.
          body: { decision: input.decision, scope: input.scope ?? "once" },
        }
      )
      if (error) throw error
      return data
    },
    // Immediate, not delayed: the person is looking at the row they just acted
    // on, and the agent behind it may already be retrying.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["get", "/approvals"] })
    },
  })
}

export function useRevokeApprovalGrant(clusterID?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (grantId: string) => {
      const { error } = await fetchFor(clusterID).DELETE(
        "/approvals/grants/{grantId}",
        { params: { path: { grantId } } }
      )
      if (error) throw error
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/approvals"]),
  })
}
