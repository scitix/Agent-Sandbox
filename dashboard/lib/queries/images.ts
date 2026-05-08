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

// Queries and mutations for the images catalog BFF API.
// GET is public; POST/PUT/DELETE are admin-only (enforced server-side too).

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import type { ImageDataset } from "@/components/images/data"
import { bff } from "@/lib/api/bff-client"

export const IMAGES_QUERY_KEY = ["images-catalog"] as const

// ─── BFF fetch helpers ────────────────────────────────────────────────────────

async function fetchImagesCatalog(): Promise<ImageDataset[]> {
  return bff.get("api/images-catalog").json()
}

async function createImageDataset(body: ImageDataset): Promise<ImageDataset> {
  return bff.post("api/images-catalog", { json: body }).json()
}

async function updateImageDataset(body: ImageDataset): Promise<ImageDataset> {
  return bff.put(`api/images-catalog/${encodeURIComponent(body.id)}`, { json: body }).json()
}

async function deleteImageDataset(id: string): Promise<void> {
  await bff.delete(`api/images-catalog/${encodeURIComponent(id)}`)
}

// ─── Query options ────────────────────────────────────────────────────────────

export function imagesCatalogQueryOptions() {
  return {
    queryKey: IMAGES_QUERY_KEY,
    queryFn: fetchImagesCatalog,
  }
}

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: createImageDataset,
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

export function useUpdateImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: updateImageDataset,
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

export function useDeleteImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: deleteImageDataset,
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

// Re-export useQuery for convenience in the page
export { useQuery }
