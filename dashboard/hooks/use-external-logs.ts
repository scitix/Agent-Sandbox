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

import { useQuery } from "@tanstack/react-query"
import { sandboxLogsConfigQueryOptions } from "@/lib/queries"

/**
 * Returns whether the external log service is configured on this BFF instance.
 * Defaults to false while loading or on error — UI hides the external log path safely.
 */
export function useExternalLogsConfigured(): boolean {
  const { data } = useQuery(sandboxLogsConfigQueryOptions())
  return data?.configured ?? false
}
