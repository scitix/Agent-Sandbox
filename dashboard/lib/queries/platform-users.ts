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

// Queries for platform-wide user counts (hub API). GET /v1/platform/users/count
// is available to any authenticated user (aggregate, desensitized count);
// GET /v1/admin/users/summary is admin-only (per-team breakdown).

import { getHubApiClient } from "@/lib/api/hub-client"

export const platformUsersCountQueryOptions = () =>
  getHubApiClient().queryOptions("get", "/v1/platform/users/count", {})

export const adminUsersSummaryQueryOptions = () =>
  getHubApiClient().queryOptions("get", "/v1/admin/users/summary", {})
