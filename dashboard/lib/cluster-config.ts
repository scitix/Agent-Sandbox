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

/**
 * Per-API base URLs behind the cluster's gateway.
 *
 * Written for the Go side's cross-cluster forwarder, but the E2B one is also
 * what the dashboard's create-sandbox path proxies to — the native `url` above
 * only reaches the control plane. Note these are gateway hostnames, which in
 * environments without public DNS resolve only via `hostAliases` below.
 */
export interface ClusterGateway {
  nativeURL?: string
  e2bURL?: string
  dataURL?: string
}

/**
 * A `hostAliases` entry from clusters.yaml — the same shape Kubernetes uses.
 *
 * These are NOT applied to the dashboard Pod (nothing sets spec.hostAliases on
 * it); they are data the config carries so in-process callers can resolve
 * gateway hostnames themselves. See `resolveHostAlias`.
 */
export interface ClusterHostAlias {
  ip: string
  hostnames: string[]
}

export interface ClusterEntry {
  id: string
  name: string
  url: string
  gateway?: ClusterGateway
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
  hostAliases?: ClusterHostAlias[]
}

const CLUSTERS_FILE = process.env.CLUSTERS_CONFIG_PATH || "/etc/agentbox/clusters.yaml"

// Cached state
let cachedClusters: ClusterEntry[] = []
let cachedHostAliases: ClusterHostAlias[] = []
let watcherInitialized = false

/**
 * Parses the whole file once. Both halves are cached together so a ConfigMap
 * update cannot leave the clusters and their host aliases out of step.
 */
function loadConfig(): { clusters: ClusterEntry[]; hostAliases: ClusterHostAlias[] } {
  try {
    const content = fs.readFileSync(CLUSTERS_FILE, "utf-8")
    const parsed = parseYaml(content) as ClustersFile | null
    if (!parsed || !Array.isArray(parsed.clusters)) {
      return { clusters: [], hostAliases: [] }
    }
    const clusters = parsed.clusters
      .filter(
        (c) =>
          c && typeof c.id === "string" && typeof c.name === "string" && typeof c.url === "string",
      )
      .map((c) => ({
        ...c,
        ...(typeof c.selector === "string" ? { selector: c.selector } : {}),
      }))
    const hostAliases = (Array.isArray(parsed.hostAliases) ? parsed.hostAliases : []).filter(
      (a): a is ClusterHostAlias =>
        !!a && typeof a.ip === "string" && Array.isArray(a.hostnames) && a.hostnames.length > 0,
    )
    return { clusters, hostAliases }
  } catch {
    return { clusters: [], hostAliases: [] }
  }
}

function ensureWatcher() {
  if (watcherInitialized) return
  watcherInitialized = true

  // Initial load
  const initial = loadConfig()
  cachedClusters = initial.clusters
  cachedHostAliases = initial.hostAliases

  const reload = () => {
    const next = loadConfig()
    cachedClusters = next.clusters
    cachedHostAliases = next.hostAliases
  }

  // Watch the directory (not subPath, so K8s ConfigMap updates are picked up)
  const dir = path.dirname(CLUSTERS_FILE)
  try {
    fs.watch(dir, (_eventType, filename) => {
      if (filename && filename === path.basename(CLUSTERS_FILE)) {
        reload()
      } else if (!filename) {
        // Some platforms don't provide filename; reload anyway
        reload()
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

/** The `hostAliases` block from clusters.yaml. Empty when the file has none. */
export function getHostAliases(): ClusterHostAlias[] {
  ensureWatcher()
  return cachedHostAliases
}

/**
 * Rewrites a gateway URL so this process can actually reach it.
 *
 * Gateway URLs use ingress hostnames. In an environment with public DNS they
 * resolve normally and this is a no-op. Without public DNS the config carries a
 * `hostAliases` block for them — but nothing puts those on the dashboard Pod's
 * `spec.hostAliases`, so the hostname is unresolvable here. Dialling the aliased
 * IP and sending the hostname as `Host` gets the request through the same
 * virtual-host routing, which is exactly what the native `url` + `headers.Host`
 * pair already does.
 *
 * Returns the URL to dial plus the `Host` header to send with it (undefined when
 * no alias applied, so the caller leaves the header alone).
 */
export function resolveHostAlias(rawUrl: string): { url: string; hostHeader?: string } {
  return applyHostAlias(rawUrl, getHostAliases())
}

/** The pure half of `resolveHostAlias`, with the alias list passed in. */
export function applyHostAlias(
  rawUrl: string,
  aliases: ClusterHostAlias[],
): { url: string; hostHeader?: string } {
  let parsed: URL
  try {
    parsed = new URL(rawUrl)
  } catch {
    return { url: rawUrl }
  }

  const alias = aliases.find((a) => a.hostnames.includes(parsed.hostname))
  if (!alias) return { url: rawUrl }

  const hostHeader = parsed.host // hostname[:port] — the vhost the ingress matches
  parsed.hostname = alias.ip
  return { url: parsed.toString(), hostHeader }
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
