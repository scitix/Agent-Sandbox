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
import { ClusterCombobox } from "@/components/cluster-combobox"
import { switchClusterPath, loginPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

export function ClusterSwitcher() {
  const router = useRouter()
  const pathname = usePathname()
  const auth = useAtomValue(authAtom)
  const currentClusterID = useClusterID()
  const setClusterID = useSetAtom(clusterIDAtom)
  const clustersData = useAtomValue(clustersAtom)
  const locale = useLocale()
  const { t } = useTranslation()

  const multiCluster = clustersData.multiCluster
  const clusters = clustersData.clusters

  // Only render in multi-cluster mode, and hide for API-key users
  // (they must re-login per cluster)
  if (!multiCluster || clusters.length === 0 || auth?.authMethod === "apikey") {
    return null
  }

  const handleSwitch = (clusterId: string | null) => {
    if (!clusterId || clusterId === currentClusterID) return

    // Compute the destination path for the new cluster, preserving the current page and locale
    const destination = switchClusterPath(pathname, clusterId)

    if (auth?.authMethod === "mock" || auth?.authMethod === "oidc") {
      // IAM/OIDC users: switch clusters seamlessly by re-resolving namespace/team,
      // then navigate to the equivalent page on the new cluster.
      setClusterID(clusterId)
      window.location.href = process.env.NEXT_PUBLIC_BASE_PATH + destination // full reload to ensure all state is cleared
    } else {
      // Fallback: redirect to login with the new cluster pre-selected.
      router.push(
        `${loginPath(locale)}?cluster=${encodeURIComponent(clusterId)}&next=${encodeURIComponent(destination)}`,
      )
    }
  }

  return (
    <div>
      <p className="text-muted-foreground mb-1 px-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
        {t("nav.cluster")}
      </p>
      <ClusterCombobox clusters={clusters} value={currentClusterID} onValueChange={handleSwitch} />
    </div>
  )
}
