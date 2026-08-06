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

// Queries and mutations for the Hub notification service (admin-only):
// daily-report config/trigger and idle-alert config/arm/disarm.

import { useQueryClient } from "@tanstack/react-query"
import { getHubApiClient } from "@/lib/api/hub-client"
import { delayedInvalidate } from "./utils"

export type GlobalNotificationConfig =
  import("@/lib/api/global-schema").components["schemas"]["NotificationConfig"]

const CONFIG_KEY = ["get", "/v1/admin/notifications/config"]

export const notificationConfigQueryOptions = () =>
  getHubApiClient().queryOptions("get", "/v1/admin/notifications/config", {})

export const notificationHistoryQueryOptions = () =>
  getHubApiClient().queryOptions(
    "get",
    "/v1/admin/notifications/history",
    {},
    { select: (data) => data.entries ?? [] },
  )

export function useUpdateNotificationConfig() {
  const qc = useQueryClient()
  return getHubApiClient().useMutation("put", "/v1/admin/notifications/config", {
    onSuccess: () => delayedInvalidate(qc, CONFIG_KEY),
  })
}

export function useTriggerDailyReport() {
  return getHubApiClient().useMutation("post", "/v1/admin/notifications/daily-report/trigger")
}

export function useArmIdleAlert() {
  const qc = useQueryClient()
  return getHubApiClient().useMutation("post", "/v1/admin/notifications/idle-alert/arm", {
    onSuccess: () => delayedInvalidate(qc, CONFIG_KEY),
  })
}

export function useDisarmIdleAlert() {
  const qc = useQueryClient()
  return getHubApiClient().useMutation("post", "/v1/admin/notifications/idle-alert/disarm", {
    onSuccess: () => delayedInvalidate(qc, CONFIG_KEY),
  })
}
