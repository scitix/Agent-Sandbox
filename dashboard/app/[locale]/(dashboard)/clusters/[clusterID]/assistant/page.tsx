"use client"

/**
 * The assistant, scoped to a cluster.
 *
 * Cluster-scoped rather than standalone because the route is what tells the
 * agent WHICH cluster the question is about: it becomes the `<page/>` marker,
 * so "what sandboxes are running?" resolves against the cluster on screen
 * rather than making the user name it.
 *
 * Note what that marker is and is not. It says where the user IS, not where to
 * look — the prompt is explicit about it — so a cluster the assistant's own
 * endpoint cannot serve produces an honest refusal from `abx` rather than the
 * wrong cluster's rows.
 */

import { useEffect } from "react"
import { useAtomValue, useSetAtom } from "jotai"
import { actingIdentityAtom } from "@/lib/atoms"
import { atomAssistantUserKey } from "@/lib/assistant/store"
import { AssistantHost } from "@/components/assistant-ui/assistant-host"
import { AssistantSurface } from "@/components/assistant-ui/assistant-surface"

export default function ClusterAssistantPage() {
  const acting = useAtomValue(actingIdentityAtom)
  const setUserKey = useSetAtom(atomAssistantUserKey)

  // Mirrored for the UI only. The gateway's own idea of who is asking comes
  // from the BFF, which overwrites it from a verified session — a value set
  // here could not authorise anything even if it were wrong.
  //
  // The ACTING identity, so an admin who selected a user sees that user's
  // workspace rather than their own empty one; the sandbox behind it is created
  // with that user's key, so this is what the server is operating on.
  useEffect(() => {
    const key = [acting.team, acting.user].filter(Boolean).join(".")
    setUserKey(key || undefined)
  }, [acting.team, acting.user, setUserKey])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AssistantHost active>
        <AssistantSurface mode="page" />
      </AssistantHost>
    </div>
  )
}
