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

import type { QuotaItem } from "@/lib/api/client"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// Provider-attached display-hint keys in the generic Quota.metadata bag. The
// Scitix quota provider populates these; other providers leave them absent, in
// which case callers fall back to the raw quota id/name (its url).
export const META_POOL_NAME = "quota.scitix.ai/pool-name"
export const META_POOL_TYPE = "quota.scitix.ai/pool-type"

// Localization keys for the known pool types (generic cloud tiers). Unknown
// values fall through to the raw string so a new provider value still renders.
const POOL_TYPE_KEYS: Record<string, TranslationKey> = {
  ondemand: "quota.poolType.ondemand",
  shared: "quota.poolType.shared",
  exclusive: "quota.poolType.exclusive",
  idle: "quota.poolType.idle",
  spot: "quota.poolType.spot",
}

/** Read the pool display hints off a quota's metadata bag. */
export function getPoolMeta(quota: QuotaItem): { poolName?: string; poolType?: string } {
  const meta = quota.metadata ?? {}
  return { poolName: meta[META_POOL_NAME], poolType: meta[META_POOL_TYPE] }
}

/** The name to show for a quota: the pool name when present, else its raw id/name. */
export function poolDisplayName(quota: QuotaItem): string {
  return getPoolMeta(quota).poolName || quota.name || quota.id
}

/** Returns a localizer for pool-type enums; unknown values pass through. */
export function usePoolTypeLabel(): (type?: string) => string | undefined {
  const { t } = useTranslation()
  return (type?: string) => {
    if (!type) return undefined
    const key = POOL_TYPE_KEYS[type]
    return key ? t(key) : type
  }
}

/** A localized pool-type pill (独占/共享/按需/闲时/竞价). Renders nothing when type is absent. */
export function PoolTypeBadge({ type, className }: { type?: string; className?: string }) {
  const label = usePoolTypeLabel()(type)
  if (!label) return null
  return (
    <span
      className={cn(
        "border-brand/40 bg-brand/10 text-brand shrink-0 border px-2 py-0.5 font-mono text-xs font-semibold tracking-wide uppercase",
        className,
      )}
    >
      {label}
    </span>
  )
}
