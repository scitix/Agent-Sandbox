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

// Query options for external sandbox log service feature gate

import { queryOptions } from "@tanstack/react-query"

const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

/**
 * Returns whether the external log service is configured on this BFF instance.
 * Cached for 5 minutes — the configuration is stable per deployment.
 */
export const sandboxLogsConfigQueryOptions = () =>
  queryOptions({
    queryKey: ["sandbox-logs-config"],
    queryFn: async () => {
      const res = await fetch(`${basePath}/api/sandbox-logs/config`)
      return res.json() as Promise<{ configured: boolean }>
    },
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
  })
