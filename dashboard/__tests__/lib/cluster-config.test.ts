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

import { describe, it, expect, afterEach, vi } from "vitest"
import * as fs from "fs"
import * as os from "os"
import * as path from "path"
import {
  applyHostAlias,
  filterClustersByVisibility,
  type ClusterEntry,
  type ClusterHostAlias,
} from "@/lib/cluster-config"

describe("filterClustersByVisibility", () => {
  const clusters: ClusterEntry[] = [
    { id: "c1", name: "Cluster 1", url: "http://c1" }, // no visible → visible to all
    { id: "c2", name: "Cluster 2", url: "http://c2", visible: "" }, // empty visible → visible to all
    { id: "c3", name: "Cluster 3", url: "http://c3", visible: "org1" }, // org only → all users in org visible
    { id: "c4", name: "Cluster 4", url: "http://c4", visible: "org1:bob,alice" }, // org + users
    { id: "c5", name: "Cluster 5", url: "http://c5", visible: "org2" }, // different org
    { id: "c6", name: "Cluster 6", url: "http://c6", visible: "org2:carol" }, // different user
  ]

  it("returns all clusters when no user context provided", () => {
    expect(filterClustersByVisibility(clusters)).toHaveLength(2) // only c1, c2 (no visible or empty)
    expect(filterClustersByVisibility(clusters, undefined, "bob")).toHaveLength(2)
    expect(filterClustersByVisibility(clusters, "org1")).toHaveLength(2)
  })

  it("returns all visible clusters for matching user in org1 org", () => {
    const result = filterClustersByVisibility(clusters, "org1", "bob")
    // c1 (no visible), c2 (empty), c3 (org=org1), c4 (org=org1 + user bob)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3", "c4"])
  })

  it("returns only org-wide visible clusters for non-listed user in org1", () => {
    const result = filterClustersByVisibility(clusters, "org1", "stranger")
    // c1, c2, c3 (org=org1, no user restriction), but NOT c4 (bob,alice only)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3"])
  })

  it("returns correct clusters for org2 org with matching user", () => {
    const result = filterClustersByVisibility(clusters, "org2", "carol")
    // c1, c2, c5 (org=org2), c6 (user carol)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c5", "c6"])
  })

  it("returns only org-wide visible clusters for non-matching user in org2", () => {
    const result = filterClustersByVisibility(clusters, "org2", "other")
    // c1, c2, c5 (org=org2), but NOT c6 (only carol)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c5"])
  })

  it("handles case-insensitive matching", () => {
    const result = filterClustersByVisibility(clusters, "org1", "bob")
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3", "c4"])
  })

  it("returns empty array when no clusters match", () => {
    const result = filterClustersByVisibility(clusters, "other-org", "anyone")
    // only c1, c2 have no visible restriction
    expect(result.map((c) => c.id)).toEqual(["c1", "c2"])
  })
})

// ─── Host aliases ─────────────────────────────────────────────────────────────

describe("applyHostAlias", () => {
  // Mirrors the hostAliases block of the hub ConfigMap's clusters.yaml.
  const aliases: ClusterHostAlias[] = [
    { ip: "10.0.0.1", hostnames: ["gw-a.example.internal"] },
    { ip: "10.0.0.2", hostnames: ["gw-b.example.internal"] },
  ]

  it("dials the aliased IP while keeping the hostname as Host", () => {
    // Without this the dashboard pod cannot resolve the gateway hostname at all:
    // hostAliases live in the ConfigMap data, not on the pod's spec.hostAliases.
    const out = applyHostAlias(
      "http://gw-a.example.internal:30080/agent-sandbox/api/e2b",
      aliases,
    )
    expect(out.url).toBe("http://10.0.0.1:30080/agent-sandbox/api/e2b")
    expect(out.hostHeader).toBe("gw-a.example.internal:30080")
  })

  it("leaves a publicly resolvable gateway untouched", () => {
    // Production gateways are real DNS names; rewriting them would break TLS.
    const url = "https://gw.example.com/agent-sandbox/api/e2b"
    const out = applyHostAlias(url, aliases)
    expect(out.url).toBe(url)
    expect(out.hostHeader).toBeUndefined()
  })

  it("is a no-op when no aliases are configured", () => {
    const url = "http://gw-a.example.internal:30080/agent-sandbox/api/e2b"
    expect(applyHostAlias(url, [])).toEqual({ url })
  })

  it("matches on hostname only, never on a substring", () => {
    const url = "http://not-gw-a.example.internal:30080/x"
    expect(applyHostAlias(url, aliases)).toEqual({ url })
  })

  it("passes a malformed url through rather than throwing", () => {
    expect(applyHostAlias("not a url", aliases)).toEqual({ url: "not a url" })
  })

  it("preserves the path and query it was given", () => {
    const out = applyHostAlias(
      "http://gw-b.example.internal:30080/agent-sandbox/api/e2b/sandboxes?x=1",
      aliases,
    )
    expect(out.url).toBe("http://10.0.0.2:30080/agent-sandbox/api/e2b/sandboxes?x=1")
  })
})

// ─── Peer sites ───────────────────────────────────────────────────────────────

describe("listPeerSites", () => {
  const tmpDirs: string[] = []

  afterEach(() => {
    for (const dir of tmpDirs.splice(0)) {
      fs.rmSync(dir, { recursive: true, force: true })
    }
    delete process.env.CLUSTERS_CONFIG_PATH
    vi.resetModules()
  })

  /**
   * The config path is read once at module load, so each case needs its own
   * file and a fresh import of the module under test. The file comes back too,
   * because a ConfigMap edit is a write to it after the module is loaded.
   */
  async function loadWith(yaml: string) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "cluster-config-"))
    tmpDirs.push(dir)
    const file = path.join(dir, "clusters.yaml")
    fs.writeFileSync(file, yaml)
    process.env.CLUSTERS_CONFIG_PATH = file
    vi.resetModules()
    return { mod: await import("@/lib/cluster-config"), file }
  }

  const clusters = `clusters:\n  - id: c1\n    name: C1\n    url: http://c1\n`

  it("parses the peerSites block", async () => {
    const { mod } = await loadWith(
      `${clusters}peerSites:\n  - name: "Console (EU)"\n    url: "https://console-eu.example.com/agentbox"\n`,
    )
    expect(mod.listPeerSites()).toEqual([
      { name: "Console (EU)", url: "https://console-eu.example.com/agentbox" },
    ])
  })

  it("is empty when the block is absent", async () => {
    const { mod } = await loadWith(clusters)
    expect(mod.listPeerSites()).toEqual([])
    // The clusters alongside it still parse — the two are cached together.
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c1"])
  })

  it("drops entries missing a name or a url", async () => {
    // Either half alone is unrenderable: a link with no label, or a label that
    // goes nowhere. Dropping beats shipping a dead entry in the picker.
    const { mod } = await loadWith(
      `${clusters}peerSites:\n  - name: "No URL"\n  - url: "https://nameless.example.com"\n  - name: "Blank URL"\n    url: ""\n  - name: "Good"\n    url: "https://good.example.com"\n`,
    )
    expect(mod.listPeerSites()).toEqual([{ name: "Good", url: "https://good.example.com" }])
  })

  // ─── Reloading after the file changes ───────────────────────────────────────
  //
  // Both of these are about a Kubernetes ConfigMap, which is the only way this
  // file is edited outside a developer's laptop. kubelet does not write it in
  // place: it materialises a new timestamped directory and swaps the `..data`
  // symlink the file points at, so the only thing that reliably differs before
  // and after an edit is the file behind the path. Reloading used to hang off
  // an `fs.watch` on the directory that matched the event's file name against
  // "clusters.yaml" — a name kubelet never emits — and the console served the
  // pre-edit configuration until its pod was restarted.

  it("picks up an edit to the file, with no restart", async () => {
    const { mod, file } = await loadWith(clusters)
    expect(mod.listPeerSites()).toEqual([])

    fs.writeFileSync(
      file,
      `${clusters}peerSites:\n  - name: "Console (CN)"\n    url: "https://console-cn.example.com/agentbox"\n`,
    )

    expect(mod.listPeerSites()).toEqual([
      { name: "Console (CN)", url: "https://console-cn.example.com/agentbox" },
    ])
  })

  it("re-reads the file the symlink points at, which is what kubelet swaps", async () => {
    const { mod, file } = await loadWith(clusters)
    const dir = path.dirname(file)
    // The same dance kubelet does: a second copy, then the symlink moved onto
    // it. The file's own name and permissions never change.
    const next = path.join(dir, "..2026_09_18_00_00_00.0000000000")
    fs.mkdirSync(next)
    fs.writeFileSync(
      path.join(next, "clusters.yaml"),
      `${clusters}peerSites:\n  - name: "Console (EU)"\n    url: "https://console-eu.example.com/agentbox"\n`,
    )
    fs.rmSync(file)
    fs.symlinkSync(path.join(path.basename(next), "clusters.yaml"), file)

    expect(mod.listPeerSites()).toEqual([
      { name: "Console (EU)", url: "https://console-eu.example.com/agentbox" },
    ])
  })

  it("keeps the last good configuration when the file cannot be read", async () => {
    const { mod, file } = await loadWith(
      `${clusters}peerSites:\n  - name: "Console (EU)"\n    url: "https://console-eu.example.com/agentbox"\n`,
    )
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c1"])

    // Mid-swap, or a mount that briefly answers with something else. Blanking
    // the cluster list here would take every cluster-scoped page down.
    fs.rmSync(file)
    fs.mkdirSync(file)
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c1"])
    expect(mod.listPeerSites()).toHaveLength(1)

    // And it recovers on its own once the file is a file again.
    fs.rmdirSync(file)
    fs.writeFileSync(file, `clusters:\n  - id: c2\n    name: C2\n    url: http://c2\n`)
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c2"])
  })

  it("keeps the last good configuration when the new one does not parse", async () => {
    const { mod, file } = await loadWith(clusters)
    // Read it once first: the cache is what has to survive the bad edit, and a
    // module that has never read the file has nothing to keep.
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c1"])
    fs.writeFileSync(file, "clusters: [ this is not a list\n")
    expect(mod.listClusters().map((c) => c.id)).toEqual(["c1"])
  })
})
