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

import { useRouter, usePathname } from "next/navigation"
import { useAtomValue, useSetAtom } from "jotai"
import { authAtom, clusterIDAtom, clustersAtom } from "@/lib/atoms"
import { ClusterCombobox, ALL_CLUSTERS_ID } from "@/components/cluster-combobox"
import { switchClusterPath, loginPath, clusterPath } from "@/lib/cluster-path"
import { useNavClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

export function ClusterSwitcher({ compact = false }: { compact?: boolean }) {
  const router = useRouter()
  const pathname = usePathname()
  const auth = useAtomValue(authAtom)
  // The route's cluster on every cluster-scoped page, the session's on the
  // cluster-agnostic one (`/admin`). `useClusterID()` would read "default"
  // there, which is a placeholder rather than a cluster.
  const currentClusterID = useNavClusterID()
  const setClusterID = useSetAtom(clusterIDAtom)
  const clustersData = useAtomValue(clustersAtom)
  const locale = useLocale()
  const { t } = useTranslation()

  const multiCluster = clustersData.multiCluster
  const clusters = clustersData.clusters
  const peerSites = clustersData.peerSites

  // Only render in multi-cluster mode. API-key sessions are included: the key
  // authenticates against every cluster, so switching needs no re-login.
  if (!multiCluster || clusters.length === 0) {
    return null
  }

  const handleSwitch = (clusterId: string | null) => {
    if (!clusterId || clusterId === currentClusterID) return

    // "All clusters" is a scope, not a cluster. The overview page reads every
    // cluster by default, and dropping the `?cluster=` param is how that
    // default is spelled — so land on the overview of the cluster the person
    // is in, with the scope reset to all. Keeping that cluster in the route is
    // the point: the sidebar's next click stays in it.
    if (clusterId === ALL_CLUSTERS_ID) {
      router.push(clusterPath(currentClusterID, "overview", locale))
      return
    }

    // Compute the destination path for the new cluster, preserving the current page and locale
    const destination = switchClusterPath(pathname, clusterId)

    if (auth?.token) {
      // Every session type spans all clusters, so switching needs no
      // re-login: mock/oidc identities are re-resolved by the BFF on each
      // request, and an API key authenticates against every cluster. A
      // missing authMethod is a pre-fix API-key session — same treatment.
      // Full reload so all cluster-scoped state is cleared.
      setClusterID(clusterId)
      window.location.href = process.env.NEXT_PUBLIC_BASE_PATH + destination
    } else {
      // Not logged in: go through login with the new cluster pre-selected.
      router.push(
        `${loginPath(locale)}?cluster=${encodeURIComponent(clusterId)}&next=${encodeURIComponent(destination)}`,
      )
    }
  }

  if (compact) {
    return (
      <ClusterCombobox
        clusters={clusters}
        value={currentClusterID}
        onValueChange={handleSwitch}
        allowAll
        peerSites={peerSites}
        inputClassName="h-8 w-[200px] font-mono text-xs"
      />
    )
  }

  return (
    <div>
      <p className="text-muted-foreground mb-1 px-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
        {t("nav.cluster")}
      </p>
      <ClusterCombobox
        clusters={clusters}
        value={currentClusterID}
        onValueChange={handleSwitch}
        peerSites={peerSites}
      />
    </div>
  )
}
