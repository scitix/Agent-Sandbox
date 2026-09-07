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
import { authAtom } from "@/lib/atoms"
import { atomAssistantUserKey } from "@/lib/assistant/store"
import { AssistantHost } from "@/components/assistant-ui/assistant-host"
import { AssistantSurface } from "@/components/assistant-ui/assistant-surface"

export default function ClusterAssistantPage() {
  const auth = useAtomValue(authAtom)
  const setUserKey = useSetAtom(atomAssistantUserKey)

  // Mirrored for the UI only. The gateway's own idea of who is asking comes
  // from the BFF, which overwrites it from a verified session — a value set
  // here could not authorise anything even if it were wrong.
  useEffect(() => {
    const key = [auth?.team, auth?.user].filter(Boolean).join(".")
    setUserKey(key || undefined)
  }, [auth?.team, auth?.user, setUserKey])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AssistantHost active>
        <AssistantSurface mode="page" />
      </AssistantHost>
    </div>
  )
}
