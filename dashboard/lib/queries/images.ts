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

// Queries and mutations for the images catalog hub API.
// All operations go through hub proxy → wsproxy.
// GET is accessible to any authenticated user; POST/PUT/DELETE are admin-only (enforced server-side).

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { getHubApiClient, getHubFetchClient } from "@/lib/api/hub-client"
import type { ImageDataset } from "@/components/images/data"

export const IMAGES_QUERY_KEY = ["images-catalog"] as const

// ─── Query options ────────────────────────────────────────────────────────────

export function imagesCatalogQueryOptions() {
  return getHubApiClient().queryOptions(
    "get",
    "/v1/images-catalog",
    {},
    {
      select: (data) => (data ?? []) as unknown as ImageDataset[],
    },
  )
}

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: ImageDataset) => {
      const { data, error } = await getHubFetchClient().POST("/v1/images-catalog", {
        body: {
          id: body.id,
          name: body.name,
          description: body.description,
          imageCount: body.imageCount,
          category: body.category,
          source: body.source,
          huggingFaceUrl: body.huggingFaceUrl,
          tags: body.tags,
          clusterDocs: body.clusterDocs,
        },
      })
      if (error) throw error
      return data
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

export function useUpdateImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: ImageDataset) => {
      const { data, error } = await getHubFetchClient().PUT("/v1/images-catalog/{id}", {
        params: { path: { id: body.id } },
        body: {
          id: body.id,
          name: body.name,
          description: body.description,
          imageCount: body.imageCount,
          category: body.category,
          source: body.source,
          huggingFaceUrl: body.huggingFaceUrl,
          tags: body.tags,
          clusterDocs: body.clusterDocs,
        },
      })
      if (error) throw error
      return data
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

export function useDeleteImageDataset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      const { error } = await getHubFetchClient().DELETE("/v1/images-catalog/{id}", {
        params: { path: { id } },
      })
      if (error) throw error
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: IMAGES_QUERY_KEY }),
  })
}

// Re-export useQuery for convenience in the page
export { useQuery }
