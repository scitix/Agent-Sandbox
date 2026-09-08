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
