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
import { featureGatesQueryOptions } from "@/lib/queries"

/**
 * Reports which optional features are wired into the selected cluster's
 * backend. Use to gate feature-specific UI so the same dashboard build works
 * against both proprietary and open-source deployments:
 *
 *   const gates = useFeatureGates()
 *   if (gates.quota) return <QuotaSelector />
 *
 * Defaults to "all off" while loading or on error, so UI hides safely instead
 * of flashing a control that may not work. The underlying query is cached for
 * 5 minutes and does not refetch on window focus — gates are stable per
 * deployment.
 */
export function useFeatureGates() {
  const { data } = useQuery(featureGatesQueryOptions())
  return {
    /** True when a non-noop quota provider is active (pool quota selector, /quotas endpoint). */
    quota: data?.quota ?? false,
    /** True when a non-noop InstanceType catalog provider is active (Env upsert sheet, /instancetypes endpoint). */
    instanceType: data?.instanceType ?? false,
    /**
     * True when a SandboxEnv may mount existing PersistentVolumeClaims
     * (Env upsert sheet volumes panel, /volumes endpoint). While false the
     * server also rejects a non-empty overrides.volumes, so this gate hides UI
     * that would otherwise produce a 400.
     */
    volumes: data?.volumes ?? false,
  }
}
