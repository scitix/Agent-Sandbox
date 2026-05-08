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
 * Mock data for the Images (Datasets & Prebuilt Environments) page.
 *
 * Design notes:
 * - `clusterDocs` maps a clusterID → Markdown usage string.
 *   If a clusterID is NOT present in the map the image is considered unavailable
 *   for that cluster and is hidden from the list page.
 * - ClusterIDs are the same identifiers returned by the BFF /clusters endpoint
 *   (e.g. "us-east-1", "ap-southeast-1").
 */

export type ImageCategory = "Benchmark" | "Gym"
export type ImageSource = "HuggingFace"

export interface ImageDataset {
  id: string
  name: string
  description: string
  /** Number of pre-built docker images available */
  imageCount: number
  category: ImageCategory
  source: ImageSource
  /** URL to the HuggingFace dataset page */
  huggingFaceUrl: string
  tags: string[]
  /** clusterID → Markdown usage documentation */
  clusterDocs: Record<string, string>
}

export const IMAGE_DATASETS: ImageDataset[] = []
