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

import * as fs from "fs"
import * as path from "path"
import { parse as parseYaml } from "yaml"
import { matchConfigEntry } from "@/lib/server/config-matcher"

export interface LogsClusterConfig {
  /**
   * Extra filters forwarded verbatim to the external log service as `{"field": {"op":"eq","value":"..."}}`
   * entries. Typical keys: `region`, `cluster`. Values are always matched with op="eq".
   */
  filters?: Record<string, string>
}

export interface ClusterEntry {
  id: string
  name: string
  url: string
  headers?: Record<string, string>
  /**
   * Full PromQL label-matcher expression that uniquely identifies this cluster's
   * metrics. Examples:
   *   - `cluster="cluster1"`                     — single label
   *   - `cluster="cluster1",region="region1"`         — multiple labels, comma separated
   * When omitted, `cluster="<id>"` is used as the default.
   */
  selector?: string
  /** Visibility filter: same format as DEX_OIDC_ADMINS ("org:user1,user2;org2"). Empty = visible to all. */
  visible?: string
  /**
   * External log service filters specific to this cluster (region, cluster label, etc.).
   * Never exposed to clients — server-side only.
   */
  logs?: LogsClusterConfig
}

interface ClustersFile {
  clusters: ClusterEntry[]
}

const CLUSTERS_FILE = process.env.CLUSTERS_CONFIG_PATH || "/etc/agentbox/clusters.yaml"

// Cached state
let cachedClusters: ClusterEntry[] = []
let watcherInitialized = false

function loadClusters(): ClusterEntry[] {
  try {
    const content = fs.readFileSync(CLUSTERS_FILE, "utf-8")
    const parsed = parseYaml(content) as ClustersFile | null
    if (!parsed || !Array.isArray(parsed.clusters)) {
      return []
    }
    return parsed.clusters
      .filter(
        (c) =>
          c && typeof c.id === "string" && typeof c.name === "string" && typeof c.url === "string",
      )
      .map((c) => ({
        ...c,
        ...(typeof c.selector === "string" ? { selector: c.selector } : {}),
      }))
  } catch {
    return []
  }
}

function ensureWatcher() {
  if (watcherInitialized) return
  watcherInitialized = true

  // Initial load
  cachedClusters = loadClusters()

  // Watch the directory (not subPath, so K8s ConfigMap updates are picked up)
  const dir = path.dirname(CLUSTERS_FILE)
  try {
    fs.watch(dir, (eventType, filename) => {
      if (filename && filename === path.basename(CLUSTERS_FILE)) {
        cachedClusters = loadClusters()
      } else if (!filename) {
        // Some platforms don't provide filename; reload anyway
        cachedClusters = loadClusters()
      }
    })
  } catch {
    // Watcher not available (e.g., file not found yet); still serve the initial load
  }
}

export function listClusters(): ClusterEntry[] {
  ensureWatcher()
  return cachedClusters
}

export function getClusterConfig(id: string): ClusterEntry | undefined {
  ensureWatcher()
  return cachedClusters.find((c) => c.id === id)
}

/**
 * Filters clusters based on user visibility.
 * - Clusters without `visible` field → visible to all (included)
 * - Clusters with `visible` field → only included if user matches
 * - If team/username not provided, only returns clusters without visible restriction
 */
export function filterClustersByVisibility(
  clusters: ClusterEntry[],
  team?: string,
  username?: string,
): ClusterEntry[] {
  // If no user context provided, only return clusters without visibility restriction
  if (!team || !username) {
    return clusters.filter((c) => !c.visible)
  }

  return clusters.filter((c) => {
    // No visible config → visible to all
    if (!c.visible) return true
    // Check if user matches the visibility config
    // Empty config = visible to all (emptyDefault: true)
    return matchConfigEntry({
      config: c.visible,
      team,
      username,
      emptyDefault: true,
    })
  })
}
