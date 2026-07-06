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

// i18n-processed-v1.1.0
import { Column, Table } from "@tanstack/react-table"
import { ChevronRightIcon, ListFilterIcon, XIcon } from "lucide-react"
import { ReactNode, useEffect, useMemo, useRef, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"

import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

import { DataTableFacetedFilterProps } from "./faceted-filter"
import { FilterEnumPanel, resolveEnumValueLabel } from "./filter-enum-panel"
import { FilterNumberPanel, formatNumberRange } from "./filter-number-panel"
import { FilterTextPanel, formatTextFilter } from "./filter-text-panel"

export interface FilterEntry<TData> {
  option: DataTableFacetedFilterProps
  column: Column<TData>
}

// Safe-triangle ("menu aim") support for the hover-opened second pane: while
// the pointer travels from the hovered dimension toward the pane, it sweeps
// over other dimension rows; switches are suppressed as long as the pointer
// stays inside the triangle formed by the leave point and the pane's left
// edge, with a grace timeout as a fallback so resting on another row still
// switches eventually. Clicks always switch immediately.
const AIM_GRACE_MS = 400

interface AimTriangle {
  ax: number
  ay: number
  bx: number
  by: number
  cx: number
  cy: number
  expires: number
}

function triangleSign(px: number, py: number, x1: number, y1: number, x2: number, y2: number) {
  return (px - x2) * (y1 - y2) - (x1 - x2) * (py - y2)
}

function pointInTriangle(px: number, py: number, t: AimTriangle): boolean {
  const d1 = triangleSign(px, py, t.ax, t.ay, t.bx, t.by)
  const d2 = triangleSign(px, py, t.bx, t.by, t.cx, t.cy)
  const d3 = triangleSign(px, py, t.cx, t.cy, t.ax, t.ay)
  const hasNeg = d1 < 0 || d2 < 0 || d3 < 0
  const hasPos = d1 > 0 || d2 > 0 || d3 > 0
  return !(hasNeg && hasPos)
}

interface DataTableFiltersProps<TData> {
  table: Table<TData>
  filterOptions: readonly DataTableFacetedFilterProps[]
}

/**
 * Single "Filters" toolbar entry: a two-pane command menu (left pane lists
 * the filter dimensions, right pane edits the selected one) plus one chip per
 * active dimension. Each chip opens the same dimension pane in its own
 * popover. Chips render purely from the column filter state, so filters
 * persisted to searchParams restore after a refresh.
 */
export function DataTableFilters<TData>({ table, filterOptions }: DataTableFiltersProps<TData>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [activeKey, setActiveKey] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const initializedDefaults = useRef(new Set<string>())
  const panelRef = useRef<HTMLDivElement>(null)
  const aimTriangleRef = useRef<AimTriangle | null>(null)
  const hoveredKeyRef = useRef<string | null>(null)
  const aimRetryRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const entries: FilterEntry<TData>[] = useMemo(
    () =>
      filterOptions.flatMap((option) => {
        const column = table.getColumn(option.columnKey)
        return column ? [{ option, column }] : []
      }),
    [filterOptions, table],
  )

  // apply configured default values once, when the filter is still empty
  useEffect(() => {
    entries.forEach(({ option, column }) => {
      if (!option.defaultValues || initializedDefaults.current.has(option.columnKey)) {
        return
      }
      initializedDefaults.current.add(option.columnKey)
      const current = column.getFilterValue()
      if (current === undefined || (Array.isArray(current) && current.length === 0)) {
        column.setFilterValue(option.defaultValues)
      }
    })
  }, [entries])

  const activeEntry = activeKey
    ? (entries.find((entry) => entry.option.columnKey === activeKey) ?? null)
    : null

  const clearAim = () => {
    aimTriangleRef.current = null
    if (aimRetryRef.current) {
      clearTimeout(aimRetryRef.current)
      aimRetryRef.current = null
    }
  }

  // drop any pending aim-retry timer on unmount
  useEffect(() => clearAim, [])

  const activateKey = (key: string) => {
    clearAim()
    setActiveKey(key)
  }

  const handleItemPointerEnter = (key: string, event: React.PointerEvent<HTMLDivElement>) => {
    hoveredKeyRef.current = key
    if (key === activeKey) return
    const triangle = aimTriangleRef.current
    const now = Date.now()
    if (
      triangle &&
      now < triangle.expires &&
      pointInTriangle(event.clientX, event.clientY, triangle)
    ) {
      // pointer is likely on its way to the second pane — hold the switch and
      // re-check after the grace period in case it actually settled here
      if (aimRetryRef.current) clearTimeout(aimRetryRef.current)
      aimRetryRef.current = setTimeout(
        () => {
          if (hoveredKeyRef.current === key) {
            activateKey(key)
          }
        },
        triangle.expires - now + 10,
      )
      return
    }
    activateKey(key)
  }

  const handleItemPointerLeave = (key: string, event: React.PointerEvent<HTMLDivElement>) => {
    if (hoveredKeyRef.current === key) {
      hoveredKeyRef.current = null
    }
    if (key !== activeKey) return
    const rect = panelRef.current?.getBoundingClientRect()
    if (!rect) return
    aimTriangleRef.current = {
      ax: event.clientX,
      ay: event.clientY,
      bx: rect.left,
      by: rect.top - 5,
      cx: rect.left,
      cy: rect.bottom + 5,
      expires: Date.now() + AIM_GRACE_MS,
    }
  }

  const handleOpenChange = (next: boolean) => {
    setOpen(next)
    if (!next) {
      setActiveKey(null)
      setSearch("")
      hoveredKeyRef.current = null
      clearAim()
    }
  }

  const activeEntries = entries.filter(({ option, column }) => {
    const value = column.getFilterValue()
    if (option.variant === "number_range") {
      return formatNumberRange(value, option.unit) !== null
    }
    if (option.variant === "text") {
      return formatTextFilter(value) !== null
    }
    return Array.isArray(value) && value.length > 0
  })

  if (entries.length === 0) {
    return null
  }

  return (
    <>
      <Popover open={open} onOpenChange={handleOpenChange} modal={true}>
        <PopoverTrigger
          render={<Button variant="outline" size="sm" className="h-9 border-dashed" />}
        >
          <ListFilterIcon className="text-muted-foreground size-4" />
          <span className="hidden text-xs font-normal sm:inline">{t("table.filters")}</span>
          <span className="sr-only">{t("table.filters")}</span>
          {activeEntries.length > 0 && (
            <Badge className="bg-secondary text-secondary-foreground rounded-sm px-1 font-mono text-xs">
              {activeEntries.length}
            </Badge>
          )}
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <div className="flex flex-row items-stretch">
            <Command shouldFilter={false} className="w-55">
              <CommandInput
                value={search}
                onValueChange={setSearch}
                placeholder={t("table.filtersSearch")}
              />
              <CommandList>
                <FilterRootMenu
                  entries={entries}
                  search={search}
                  activeKey={activeKey}
                  onSelect={activateKey}
                  onItemPointerEnter={handleItemPointerEnter}
                  onItemPointerLeave={handleItemPointerLeave}
                />
              </CommandList>
            </Command>
            {/* second level opens beside the dimension list */}
            {activeEntry && (
              <div ref={panelRef} className="border-border w-65 border-l" onPointerEnter={clearAim}>
                <FilterDimensionPanel
                  key={activeEntry.option.columnKey}
                  entry={activeEntry}
                  onApplied={() => handleOpenChange(false)}
                />
              </div>
            )}
          </div>
        </PopoverContent>
      </Popover>
      {activeEntries.map((entry) => (
        <FilterChip key={entry.option.columnKey} entry={entry} />
      ))}
    </>
  )
}

/** Dispatch the editor pane for one filter dimension by variant. */
export function FilterDimensionPanel<TData>({
  entry,
  onApplied,
  autoFocusInput = false,
}: {
  entry: FilterEntry<TData>
  onApplied: () => void
  autoFocusInput?: boolean
}) {
  if (entry.option.variant === "number_range") {
    return (
      <FilterNumberPanel
        column={entry.column}
        option={entry.option}
        onApplied={onApplied}
        autoFocusInput={autoFocusInput}
      />
    )
  }
  if (entry.option.variant === "text") {
    return (
      <FilterTextPanel
        column={entry.column}
        option={entry.option}
        onApplied={onApplied}
        autoFocusInput={autoFocusInput}
      />
    )
  }
  return (
    <FilterEnumPanel column={entry.column} option={entry.option} autoFocusSearch={autoFocusInput} />
  )
}

interface FilterRootMenuProps<TData> {
  entries: FilterEntry<TData>[]
  search: string
  activeKey: string | null
  onSelect: (key: string) => void
  onItemPointerEnter: (key: string, event: React.PointerEvent<HTMLDivElement>) => void
  onItemPointerLeave: (key: string, event: React.PointerEvent<HTMLDivElement>) => void
}

function FilterRootMenu<TData>({
  entries,
  search,
  activeKey,
  onSelect,
  onItemPointerEnter,
  onItemPointerLeave,
}: FilterRootMenuProps<TData>) {
  const { t } = useTranslation()
  const query = search.trim().toLowerCase()
  const visibleEntries = query
    ? entries.filter(({ option }) =>
        (option.title ?? option.columnKey).toLowerCase().includes(query),
      )
    : entries

  if (visibleEntries.length === 0) {
    return <div className="py-6 text-center text-sm">{t("table.noResults")}</div>
  }

  return (
    <CommandGroup>
      {visibleEntries.map(({ option, column }) => {
        let summary: ReactNode = null
        if (option.variant === "number_range") {
          const text = formatNumberRange(column.getFilterValue(), option.unit)
          if (text !== null) {
            summary = (
              <Badge variant="secondary" className="rounded-sm px-1 font-mono text-xs">
                {text}
              </Badge>
            )
          }
        } else if (option.variant === "text") {
          const text = formatTextFilter(column.getFilterValue())
          if (text !== null) {
            summary = (
              <Badge
                variant="secondary"
                className="max-w-32 truncate rounded-sm px-1 font-mono text-xs"
              >
                {text}
              </Badge>
            )
          }
        } else {
          const value = column.getFilterValue()
          const count = Array.isArray(value) ? value.length : 0
          if (count > 0) {
            summary = (
              <Badge variant="secondary" className="rounded-sm px-1 font-mono text-xs">
                {count}
              </Badge>
            )
          }
        }
        return (
          <CommandItem
            key={option.columnKey}
            value={option.columnKey}
            className={cn(
              "cursor-pointer [&>svg:last-child]:hidden",
              activeKey === option.columnKey && "bg-muted",
            )}
            onSelect={() => onSelect(option.columnKey)}
            onPointerEnter={(event) => onItemPointerEnter(option.columnKey, event)}
            onPointerLeave={(event) => onItemPointerLeave(option.columnKey, event)}
          >
            <span className="truncate">{option.title ?? option.columnKey}</span>
            <span className="ml-auto flex items-center gap-1">
              {summary}
              <ChevronRightIcon className="text-muted-foreground size-4" />
            </span>
          </CommandItem>
        )
      })}
    </CommandGroup>
  )
}

/**
 * One active-filter chip: `title · <value badges>`. The chip body opens the
 * dimension pane in its own popover for in-place editing; the trailing X
 * clears just this dimension.
 */
function FilterChip<TData>({ entry }: { entry: FilterEntry<TData> }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const { option, column } = entry
  const title = option.title ?? option.columnKey

  let valueBadges: ReactNode = null
  if (option.variant === "number_range") {
    valueBadges = (
      <Badge variant="secondary" className="rounded-sm px-1 font-normal">
        {formatNumberRange(column.getFilterValue(), option.unit)}
      </Badge>
    )
  } else if (option.variant === "text") {
    valueBadges = (
      <Badge variant="outline" className="rounded-sm px-1 font-normal">
        {formatTextFilter(column.getFilterValue())}
      </Badge>
    )
  } else {
    const values = Array.isArray(column.getFilterValue())
      ? (column.getFilterValue() as (string | undefined)[])
      : []
    valueBadges =
      values.length > 2 ? (
        <Badge className="rounded-sm px-1 font-normal" variant="outline">
          <span className="font-mono">{values.length}</span>
          {t("table.itemsSelectedSuffix")}
        </Badge>
      ) : (
        values.map((value, index) => (
          <Badge
            key={index}
            className="rounded-sm px-1 font-normal"
            variant={
              option.renderer !== undefined
                ? "outline"
                : option.options?.find((o) => o.value === value)?.isDestructive
                  ? "destructive"
                  : "outline"
            }
          >
            {resolveEnumValueLabel(option, value, t)}
          </Badge>
        ))
      )
  }

  return (
    <div className="bg-background flex h-9 shrink-0 items-stretch overflow-hidden rounded-md border shadow-xs">
      <Popover open={open} onOpenChange={setOpen} modal={true}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="hover:bg-muted flex min-w-0 cursor-pointer items-center gap-1 px-2 text-xs"
            />
          }
        >
          <span className="text-muted-foreground shrink-0">{title}</span>
          <span className="text-muted-foreground shrink-0">·</span>
          <span className="flex max-w-56 items-center gap-1 truncate">{valueBadges}</span>
        </PopoverTrigger>
        <PopoverContent className="w-65 p-0" align="start">
          <FilterDimensionPanel entry={entry} onApplied={() => setOpen(false)} autoFocusInput />
        </PopoverContent>
      </Popover>
      <Separator orientation="vertical" />
      <button
        type="button"
        onClick={() => column.setFilterValue(undefined)}
        aria-label={t("table.clearFilter")}
        className="hover:bg-muted text-muted-foreground hover:text-foreground flex cursor-pointer items-center px-1.5"
      >
        <XIcon className="size-3.5" />
      </button>
    </div>
  )
}
