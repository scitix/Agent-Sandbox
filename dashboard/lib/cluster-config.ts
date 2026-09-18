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
import { parse as parseYaml } from "yaml"
import { matchConfigEntry } from "@/lib/server/config-matcher"

export interface LogsClusterConfig {
  /**
   * Extra filters forwarded verbatim to the external log service as `{"field": {"op":"eq","value":"..."}}`
   * entries. Typical keys: `region`, `cluster`. Values are always matched with op="eq".
   */
  filters?: Record<string, string>
  /**
   * This cluster's container output is sharded across two log stores by
   * namespace, so the store has to be chosen per query rather than per
   * deployment. See `projectForNamespace`.
   *
   * Getting it wrong is silent: the service answers 200 with an empty body for
   * a store that holds nothing, which is indistinguishable from a pod that
   * printed nothing.
   */
  splitProject?: boolean
}

/**
 * Log-store sharding, for deployments whose central service splits container
 * output across more than one store.
 *
 * Mirrors the Go side (`pkg/utils/logclient.ProjectFor`) — both halves of the
 * platform query the same gateway and have to agree on which store holds what.
 */
export const TENANT_NAMESPACE_PREFIX = "t-"
export const TENANT_PROJECT = "default"
export const PLATFORM_PROJECT = "internal"

/**
 * Picks the log store to query for one namespace.
 *
 * Returns undefined when the cluster is not sharded, which leaves the
 * endpoint's own project in force — the single-store case, and the one every
 * cluster looked like before sharding existed.
 */
export function projectForNamespace(
  splitProject: boolean | undefined,
  namespace: string,
): string | undefined {
  if (!splitProject) return undefined
  return namespace.startsWith(TENANT_NAMESPACE_PREFIX) ? TENANT_PROJECT : PLATFORM_PROJECT
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

/**
 * Another Dashboard deployment the user can jump to from the cluster picker.
 *
 * A deployment only ever sees the clusters it is configured with, so two
 * geographically separated control planes are two separate consoles with no way
 * to reach each other. This is that way: each side lists the other, and the
 * entry appears under the cluster list as a plain link out. It is deliberately
 * NOT a cluster — nothing here is fetched, authenticated against or proxied to;
 * clicking it leaves this deployment.
 */
export interface PeerSite {
  /** Shown verbatim in the picker. Operators write it in whatever language they run in. */
  name: string
  /** Absolute URL of the other Dashboard, e.g. `https://console.example.com/agentbox`. */
  url: string
}

interface ClustersFile {
  clusters: ClusterEntry[]
  hostAliases?: ClusterHostAlias[]
  peerSites?: PeerSite[]
}

const CLUSTERS_FILE = process.env.CLUSTERS_CONFIG_PATH || "/etc/agentbox/clusters.yaml"

interface ConfigSnapshot {
  clusters: ClusterEntry[]
  hostAliases: ClusterHostAlias[]
  peerSites: PeerSite[]
}

// Cached state. The three parts are cached together so a ConfigMap update
// cannot leave the clusters and their host aliases out of step.
let cached: ConfigSnapshot = { clusters: [], hostAliases: [], peerSites: [] }
/**
 * Identity of the file the cache was read from — device, inode, mtime, size.
 * Null until a read succeeds, which is what makes the first access load.
 */
let cachedKey: string | null = null

/**
 * Parses the whole file. Returns null when the text is not valid YAML, which is
 * a different answer from a file that parses to nothing: the first means "I
 * cannot tell what this says", the second means "there are no clusters".
 */
function parseConfig(content: string): ConfigSnapshot | null {
  let parsed: ClustersFile | null
  try {
    parsed = parseYaml(content) as ClustersFile | null
  } catch {
    return null
  }
  if (!parsed || !Array.isArray(parsed.clusters)) {
    return { clusters: [], hostAliases: [], peerSites: [] }
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
  // Both fields are required: an entry missing either would render as a link
  // with no label or a label that goes nowhere.
  const peerSites = (Array.isArray(parsed.peerSites) ? parsed.peerSites : []).filter(
    (s): s is PeerSite => !!s && typeof s.name === "string" && typeof s.url === "string" && !!s.url,
  )
  return { clusters, hostAliases, peerSites }
}

/**
 * Identity of the file behind the path, or null when there is nothing there.
 *
 * `statSync` follows the symlink, which is the whole point: a Kubernetes
 * ConfigMap volume never writes this file in place. kubelet materialises a new
 * timestamped directory and atomically swaps the `..data` symlink that
 * `clusters.yaml` points at, so the file's own inode is the thing that changes.
 */
function configKey(): string | null {
  try {
    const s = fs.statSync(CLUSTERS_FILE)
    return `${s.dev}:${s.ino}:${s.mtimeMs}:${s.size}`
  } catch {
    return null
  }
}

/**
 * Re-reads the file when it is no longer the one the cache came from.
 *
 * Called on every read, which is affordable for a file of a few kilobytes that
 * changes once a release, and it is the mechanism that has to be right: an
 * `fs.watch` on this directory sees `..data`, `..data_tmp` and the new
 * `..2026_…` directory — never `clusters.yaml` — so matching on the file name
 * (what this used to do, with a watcher as the only reload path) matched
 * nothing and a ConfigMap edit appeared only after a pod restart.
 *
 * A read that fails, or a file that does not parse, leaves the last good
 * configuration in place rather than blanking the cluster list: the console
 * cannot tell "someone is mid-write" from "there are no clusters", and one of
 * those guesses takes every cluster-scoped page down.
 */
function refresh(): void {
  const key = configKey()
  if (key === null || key === cachedKey) return
  let content: string
  try {
    content = fs.readFileSync(CLUSTERS_FILE, "utf-8")
  } catch {
    return
  }
  const parsed = parseConfig(content)
  if (!parsed) return
  cached = parsed
  cachedKey = key
}

export function listClusters(): ClusterEntry[] {
  refresh()
  return cached.clusters
}

export function getClusterConfig(id: string): ClusterEntry | undefined {
  refresh()
  return cached.clusters.find((c) => c.id === id)
}

/** The `peerSites` block from clusters.yaml. Empty when the file has none. */
export function listPeerSites(): PeerSite[] {
  refresh()
  return cached.peerSites
}

/** The `hostAliases` block from clusters.yaml. Empty when the file has none. */
export function getHostAliases(): ClusterHostAlias[] {
  refresh()
  return cached.hostAliases
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
