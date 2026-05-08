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

import { NextResponse } from "next/server"

// This legacy BFF route has been superseded by /api/clusters/[clusterID]/[...path].
// All API requests from the dashboard now go through the cluster-aware route.
// Return 410 Gone to prompt clients to update.

const GONE_RESPONSE = NextResponse.json(
  {
    error: "This API route has been deprecated. Use /api/clusters/{clusterID}/... instead.",
    migrate: "/api/clusters/default/...",
  },
  { status: 410 },
)

export function GET() {
  return GONE_RESPONSE
}

export function POST() {
  return GONE_RESPONSE
}

export function PUT() {
  return GONE_RESPONSE
}

export function DELETE() {
  return GONE_RESPONSE
}

export function PATCH() {
  return GONE_RESPONSE
}
