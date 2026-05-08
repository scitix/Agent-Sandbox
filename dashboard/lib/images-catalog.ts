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

/**
 * Server-side images catalog loader.
 *
 * In development / without a ConfigMap:  reads from the static TypeScript file
 * (components/images/data.ts compiled values injected at startup).
 *
 * In production:  reads from IMAGES_CATALOG_PATH (default /etc/agentbox/images-catalog.json).
 * The file is a JSON array of ImageDataset objects.  When the ConfigMap is mounted k8s will
 * update the symlink atomically, so we watch the directory just like cluster-config.ts.
 *
 * The in-memory static data acts as a fallback when the file is missing or empty.
 */
import * as fs from "fs"
import * as path from "path"
import { IMAGE_DATASETS, type ImageDataset } from "@/components/images/data"

const CATALOG_FILE = process.env.IMAGES_CATALOG_PATH || "/etc/agentbox/images-catalog.json"

let cachedCatalog: ImageDataset[] | null = null
let watcherInitialized = false

function loadFromFile(): ImageDataset[] | null {
  try {
    const content = fs.readFileSync(CATALOG_FILE, "utf-8").trim()
    if (!content) return null
    const parsed = JSON.parse(content) as unknown
    if (!Array.isArray(parsed) || parsed.length === 0) return null
    return parsed as ImageDataset[]
  } catch {
    return null
  }
}

export function ensureWatcher(): void {
  if (watcherInitialized) return
  watcherInitialized = true

  cachedCatalog = loadFromFile()

  const dir = path.dirname(CATALOG_FILE)
  try {
    fs.watch(dir, (_eventType, filename) => {
      if (!filename || filename === path.basename(CATALOG_FILE)) {
        cachedCatalog = loadFromFile()
      }
    })
  } catch {
    // Not fatal — serve static fallback
  }
}

/** Returns the current catalog: file-backed if available, otherwise static fallback. */
export function listImages(): ImageDataset[] {
  ensureWatcher()
  return cachedCatalog ?? IMAGE_DATASETS
}

/** Persists the full catalog to the file (admin writes only). */
export function saveImages(datasets: ImageDataset[]): void {
  try {
    // Ensure directory exists
    const dir = path.dirname(CATALOG_FILE)
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(CATALOG_FILE, JSON.stringify(datasets, null, 2), "utf-8")
    cachedCatalog = datasets
  } catch (e) {
    throw new Error(`Failed to write images catalog: ${(e as Error).message}`)
  }
}
