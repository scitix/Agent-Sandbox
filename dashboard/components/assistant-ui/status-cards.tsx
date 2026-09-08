"use client"

/**
 * The landing screen's status board: one card per live condition, each a
 * ready-made question.
 *
 * A card is a QUESTION, not a link. Clicking navigates to the page the number
 * lives on AND asks the assistant about it, so the answer arrives beside the
 * rows it is about.
 *
 * Shape of the contract, and why:
 *   * A GROUP is a dimension of the deployment (environments, sandboxes, warm
 *     pools, templates). It owns the type label, the destination page and the
 *     accent, so every card derived from it is recognisably the same kind of
 *     thing.
 *   * Its BUCKETS come from a query, and the queries are the SAME ones the list
 *     pages use — identical `queryKey`, so the board costs no extra requests and
 *     a card can never disagree with its page about what "ready" means.
 *   * A bucket with a count of zero is dropped. There is no "0 environments not
 *     ready" tile; a healthy deployment simply has a shorter board.
 */

import type { FC } from "react"
import { useQuery, type UseQueryOptions } from "@tanstack/react-query"

import { Skeleton } from "@/components/ui/skeleton"
import { useClusterID } from "@/hooks/use-cluster-id"
import { envsQueryOptions } from "@/lib/queries/env"
import { sandboxesQueryOptions } from "@/lib/queries/sandbox"
import { templatesQueryOptions } from "@/lib/queries/template"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { DashboardPage } from "@/lib/cluster-path"

/**
 * The four wordings a bucket needs, keyed off one name.
 *
 * The strings are resolved where they are rendered rather than baked into the
 * bucket: `select` runs inside react-query's cache and would freeze whatever
 * locale was active when the data first landed, so switching language would
 * leave the board in the old one until something invalidated the query.
 */
type BucketCopy =
  | "envsReady"
  | "envsNotReady"
  | "sandboxesRunning"
  | "poolsIdle"
  | "poolsUnavailable"
  | "templates"

/** One card: a live count and which wordings describe it. */
export interface StatusBucket {
  /** Unique within its group. */
  key: string
  copy: BucketCopy
  count: number
}

/**
 * The card face's colour scheme. Named rather than free-form Tailwind so a board
 * cannot drift into a fifth blue: the gradient and the number of one group both
 * come from the same entry.
 */
export type StatusAccent = "sky" | "violet" | "amber" | "emerald" | "rose"

const ACCENTS: Record<StatusAccent, { cover: string; text: string }> = {
  sky: {
    cover:
      "from-sky-200/80 via-indigo-100 to-cyan-100 dark:from-sky-500/30 dark:via-indigo-500/20 dark:to-cyan-500/10",
    text: "text-sky-600 dark:text-sky-300",
  },
  violet: {
    cover:
      "from-violet-200/80 via-fuchsia-100 to-indigo-100 dark:from-violet-500/30 dark:via-fuchsia-500/20 dark:to-indigo-500/10",
    text: "text-violet-600 dark:text-violet-300",
  },
  amber: {
    cover:
      "from-amber-200/80 via-orange-100 to-rose-100 dark:from-amber-500/30 dark:via-orange-500/20 dark:to-rose-500/10",
    text: "text-amber-600 dark:text-amber-300",
  },
  emerald: {
    cover:
      "from-emerald-200/80 via-teal-100 to-sky-100 dark:from-emerald-500/30 dark:via-teal-500/20 dark:to-sky-500/10",
    text: "text-emerald-600 dark:text-emerald-300",
  },
  rose: {
    cover:
      "from-rose-200/80 via-pink-100 to-violet-100 dark:from-rose-500/30 dark:via-pink-500/20 dark:to-violet-500/10",
    text: "text-rose-600 dark:text-rose-300",
  },
}

/** Where a card sends you, and what it asks once you are there. */
export interface StatusAsk {
  page: DashboardPage
  prompt: string
}

interface StatusGroupSpec {
  key: string
  typeLabelKey: TranslationKey
  accent: StatusAccent
  page: DashboardPage
  buckets: () => UseQueryOptions<unknown, Error, StatusBucket[], readonly unknown[]>
}

const bucket = (key: string, count: number, copy: BucketCopy): StatusBucket => ({
  key,
  count,
  copy,
})

const copyKey = (copy: BucketCopy, part: "label" | "description" | "prompt") =>
  `assistant.status.${copy}.${part}` as TranslationKey

/** Buckets with nothing in them never become cards. */
const nonEmpty = (items: StatusBucket[]) => items.filter(b => b.count > 0)

/**
 * Environments and pools come from ONE request.
 *
 * `SandboxEnvStatus` already carries the env-wide sums (`idleReplicas`,
 * `runningReplicas`, `desiredReplicas`), so the pool board needs no per-pool
 * fan-out — which would have been one request per environment for numbers the
 * list endpoint had already added up.
 */
function envBucketsQuery() {
  const base = envsQueryOptions()
  return {
    ...base,
    select: (data: unknown) => {
      const envs = (data as { items?: EnvLike[] })?.items ?? []
      const ready = envs.filter(isEnvReady).length
      return nonEmpty([
        bucket("ready", ready, "envsReady"),
        bucket("notReady", envs.length - ready, "envsNotReady"),
      ])
    },
  } as unknown as UseQueryOptions<unknown, Error, StatusBucket[], readonly unknown[]>
}

function poolBucketsQuery() {
  const base = envsQueryOptions()
  return {
    ...base,
    select: (data: unknown) => {
      const envs = (data as { items?: EnvLike[] })?.items ?? []
      const sum = (pick: (s: NonNullable<EnvLike["status"]>) => number | undefined) =>
        envs.reduce((n, e) => n + (e.status ? (pick(e.status) ?? 0) : 0), 0)
      const idle = sum(s => s.idleReplicas)
      // What was asked for but is not idle and not serving anything: pods that
      // exist on paper and cannot be claimed.
      const unavailable = Math.max(
        0,
        sum(s => s.desiredReplicas) - idle - sum(s => s.runningReplicas)
      )
      return nonEmpty([
        bucket("idle", idle, "poolsIdle"),
        bucket("unavailable", unavailable, "poolsUnavailable"),
      ])
    },
  } as unknown as UseQueryOptions<unknown, Error, StatusBucket[], readonly unknown[]>
}

function sandboxBucketsQuery() {
  const base = sandboxesQueryOptions()
  return {
    ...base,
    // The list query already unwraps `items`; re-declaring `select` replaces
    // that, so this reads the raw envelope rather than the unwrapped array.
    select: (data: unknown) => {
      const items = (data as { items?: { status?: string }[] })?.items ?? []
      // Capitalised: the API's phase enum is `Pending | Starting | Running | …`,
      // and a lower-case comparison here matches nothing — which shows up as a
      // card that is simply never there rather than as an error.
      const running = items.filter(s => s.status === "Running").length
      return nonEmpty([bucket("running", running, "sandboxesRunning")])
    },
  } as unknown as UseQueryOptions<unknown, Error, StatusBucket[], readonly unknown[]>
}

function templateBucketsQuery() {
  const base = templatesQueryOptions()
  return {
    ...base,
    select: (data: unknown) => {
      const items = (data as { items?: unknown[] })?.items ?? []
      return nonEmpty([bucket("all", items.length, "templates")])
    },
  } as unknown as UseQueryOptions<unknown, Error, StatusBucket[], readonly unknown[]>
}

interface EnvLike {
  status?: {
    conditions?: { type: string; status: string }[]
    idleReplicas?: number
    runningReplicas?: number
    desiredReplicas?: number
  }
}

const isEnvReady = (env: EnvLike) =>
  env.status?.conditions?.some(c => c.type === "Ready" && c.status === "True") ?? false

const GROUPS: StatusGroupSpec[] = [
  {
    key: "envs",
    typeLabelKey: "assistant.status.type.envs",
    accent: "sky",
    page: "envs",
    buckets: envBucketsQuery,
  },
  {
    key: "sandboxes",
    typeLabelKey: "assistant.status.type.sandboxes",
    accent: "violet",
    page: "sandboxes",
    buckets: sandboxBucketsQuery,
  },
  {
    key: "pools",
    typeLabelKey: "assistant.status.type.pools",
    accent: "emerald",
    page: "envs",
    buckets: poolBucketsQuery,
  },
  {
    key: "templates",
    typeLabelKey: "assistant.status.type.templates",
    accent: "amber",
    page: "templates",
    buckets: templateBucketsQuery,
  },
]

/**
 * The board. Renders nothing without a cluster — which is also what makes the
 * cluster-less landing collapse to a centred composer.
 */
export const StatusCards: FC<{ onAsk: (ask: StatusAsk) => void }> = ({
  onAsk,
}) => {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  if (!clusterID) return null
  return (
    <section className="flex w-full flex-col gap-3">
      <h2 className="flex h-7 items-center gap-2 text-[13px] font-medium">
        {t("assistant.status.title")}
      </h2>
      {/* The heading stays in the landing's column; the cards are the one thing
          allowed out of it. Three tiles across want more than a reading measure,
          so the grid bleeds 4rem past each edge — but only from `@4xl`, where
          the pane is wide enough that the bleed lands in the margin instead of
          pushing a horizontal scrollbar under the composer. */}
      <div className="@md:grid-cols-2 @3xl:grid-cols-3 @4xl:-mx-16 grid gap-4">
        {GROUPS.map(group => (
          <StatusGroup key={group.key} spec={group} onAsk={onAsk} />
        ))}
      </div>
    </section>
  )
}

/** One dimension's cards. A group with nothing to report contributes nothing. */
function StatusGroup({
  spec,
  onAsk,
}: {
  spec: StatusGroupSpec
  onAsk: (ask: StatusAsk) => void
}) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery(spec.buckets())
  if (isLoading) return <Skeleton className="h-56 rounded-2xl" />
  return (
    <>
      {(data ?? []).map(b => (
        <StatusCard
          key={`${spec.key}:${b.key}`}
          spec={spec}
          bucket={b}
          onClick={() =>
            onAsk({ page: spec.page, prompt: t(copyKey(b.copy, "prompt")) })
          }
        />
      ))}
    </>
  )
}

function StatusCard({
  spec,
  bucket,
  onClick,
}: {
  spec: StatusGroupSpec
  bucket: StatusBucket
  onClick: () => void
}) {
  const { t } = useTranslation()
  const accent = ACCENTS[spec.accent]
  return (
    <button
      type="button"
      onClick={onClick}
      className="bg-card hover:border-primary/30 group flex h-full flex-col gap-3 rounded-2xl border p-2 text-left transition-shadow hover:shadow-md"
    >
      {/* Nested radii: the cover is inset inside the card and rounded a step
          tighter, and the plate inside it a step tighter again. That is what
          makes the stack read as depth rather than as three boxes. */}
      <div
        className={cn(
          "bg-linear-to-br relative flex h-32 items-end overflow-hidden rounded-xl px-5 pt-5",
          accent.cover
        )}
      >
        {/* The "thumbnail": what this card is a picture of. A count and the
            shape of the list behind it, clipped at the bottom edge exactly as a
            screenshot would be. */}
        <div className="bg-card/85 flex w-full flex-col gap-2.5 rounded-t-xl px-4 pb-4 pt-3.5 shadow-sm backdrop-blur-sm">
          <div className="flex items-baseline gap-2">
            <span
              className={cn(
                "text-2xl font-medium leading-none tracking-[-0.02em] tabular-nums",
                accent.text
              )}
            >
              {bucket.count}
            </span>
            <span className="text-muted-foreground min-w-0 truncate text-[11px]">
              {t(spec.typeLabelKey)}
            </span>
          </div>
          {/* Rows standing in for the list this number came from. Decorative on
              purpose — inventing a real chart for one figure would be a lie. */}
          <div aria-hidden className="flex flex-col gap-1.5">
            <span className="bg-foreground/10 h-1.5 w-full rounded-full" />
            <span className="bg-foreground/8 h-1.5 w-4/5 rounded-full" />
            <span className="bg-foreground/6 h-1.5 w-3/5 rounded-full" />
          </div>
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-1.5 px-1.5 pb-1.5">
        {/* "3 environments not ready" — the measure word is the locale's
            business, so the count and the noun are one interpolated string
            rather than two spans the layout would have to space by hand. */}
        <div className="truncate text-[15px] font-medium">
          {t("assistant.status.cardTitle", {
            count: String(bucket.count),
            label: t(copyKey(bucket.copy, "label")),
          })}
        </div>
        <p className="text-muted-foreground line-clamp-2 text-[13px] leading-snug">
          {t(copyKey(bucket.copy, "description"))}
        </p>
      </div>
    </button>
  )
}
