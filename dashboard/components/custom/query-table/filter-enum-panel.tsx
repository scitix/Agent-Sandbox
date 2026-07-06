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
import { Column } from "@tanstack/react-table"
import { ArrowLeftRightIcon, CheckCheckIcon, XIcon } from "lucide-react"
import { isValidElement, ReactNode, useMemo, useState } from "react"

import { Checkbox } from "@/components/ui/checkbox"
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"

import { useTranslation } from "@/lib/i18n"

import { DataTableFacetedFilterDefaultProps, DataTableFacetedFilterOption } from "./faceted-filter"

type Translator = ReturnType<typeof useTranslation>["t"]

/**
 * Flatten a label ReactNode to its plain text, recursing into elements (e.g.
 * badges). Lets the search match what the user actually sees.
 */
function reactNodeToText(node: ReactNode): string {
  if (node === null || node === undefined || typeof node === "boolean") {
    return ""
  }
  if (typeof node === "string" || typeof node === "number") {
    return String(node)
  }
  if (Array.isArray(node)) {
    return node.map(reactNodeToText).join(" ")
  }
  if (isValidElement(node)) {
    return reactNodeToText((node.props as { children?: ReactNode }).children)
  }
  return ""
}

/**
 * Lowercased haystack a search query is matched against for one option: the
 * raw value plus the rendered label text, so users can search by either the
 * stored value ("Running") or its translated label.
 */
export function optionSearchText(option: DataTableFacetedFilterOption): string {
  return `${String(option.value ?? "")} ${reactNodeToText(option.label)}`.toLowerCase()
}

/**
 * Resolve the display label for a single filter value, preferring the
 * statically configured options, then the custom renderer, then the raw
 * value, then the empty-option / unknown fallbacks.
 */
export function resolveEnumValueLabel(
  option: DataTableFacetedFilterDefaultProps,
  value: string | undefined,
  t: Translator,
): ReactNode {
  const fromOptions = option.options?.find((o) => o.value === value)
  if (fromOptions) {
    return fromOptions.label
  }
  let label: ReactNode = value
  if (option.renderer && value !== undefined) {
    label = option.renderer(value)
  }
  return label || option.emptyOption?.label || t("common.unknown")
}

interface FilterEnumPanelProps<TData> {
  column: Column<TData>
  option: DataTableFacetedFilterDefaultProps
  // focus the search input on mount (skip in the hover-opened flyout, where
  // stealing focus would interrupt typing in the dimension search)
  autoFocusSearch?: boolean
}

/**
 * Self-contained option picker for one enum filter dimension: its own search
 * input plus checkbox rows and select-all / invert / clear actions. Used both
 * as the second-level pane of the Filters menu and inside an active-filter
 * chip popover.
 */
export function FilterEnumPanel<TData>({
  column,
  option,
  autoFocusSearch = false,
}: FilterEnumPanelProps<TData>) {
  const { t } = useTranslation()
  const [search, setSearch] = useState("")
  const facets = column.getFacetedUniqueValues()
  const selectedValues = new Set((column.getFilterValue() as string[] | undefined) ?? [])

  const options: DataTableFacetedFilterOption[] = useMemo(() => {
    if (option.options) {
      return option.options.filter((o) => 0 < (facets?.get(o.value) || 0))
    }
    // Derive options from facets if no static options provided. Facet keys are
    // whatever the column accessor returns, which may be non-string — null /
    // undefined for absent cells, or arrays from multi-value columns. Coerce
    // each to a stable string (null/undefined collapse to ""), so it can serve
    // as a unique React key and cmdk item value; collisions there both warn and
    // make cmdk treat several rows as the same item (all appear selected).
    // Equivalent keys are merged and their counts summed.
    const merged = new Map<string, number>()
    for (const [value, count] of facets || []) {
      const key = value == null ? "" : String(value)
      merged.set(key, (merged.get(key) || 0) + count)
    }
    return Array.from(merged)
      .map(([value, count]) => ({
        label: resolveEnumValueLabel(option, value, t),
        value,
        isDestructive: false,
        count,
      }))
      .sort((a, b) => (b.count || 0) - (a.count || 0))
  }, [option, facets, t])

  const query = search.trim().toLowerCase()
  const visibleOptions = query
    ? options.filter((o) => optionSearchText(o).includes(query))
    : options

  const applyValues = (values: Set<string>) => {
    const filterValues = Array.from(values)
    column.setFilterValue(filterValues.length ? filterValues : undefined)
  }

  const toggleValue = (value: string) => {
    const next = new Set(selectedValues)
    if (next.has(value)) {
      next.delete(value)
    } else {
      next.add(value)
    }
    applyValues(next)
  }

  // Select-all / invert act on the currently visible (search-filtered)
  // options only, merged into the existing selection.
  const selectAllVisible = () => {
    const next = new Set(selectedValues)
    visibleOptions.forEach((o) => next.add(o.value))
    applyValues(next)
  }

  const invertVisible = () => {
    const next = new Set(selectedValues)
    visibleOptions.forEach((o) => {
      if (next.has(o.value)) {
        next.delete(o.value)
      } else {
        next.add(o.value)
      }
    })
    applyValues(next)
  }

  return (
    <Command shouldFilter={false}>
      <CommandInput
        autoFocus={autoFocusSearch}
        value={search}
        onValueChange={setSearch}
        placeholder={t("table.searchFieldEllipsis", {
          field: (option.title ?? option.columnKey).toLowerCase(),
        })}
      />
      <CommandList>
        {visibleOptions.length === 0 ? (
          <div className="py-6 text-center text-sm">{t("table.noResults")}</div>
        ) : (
          <CommandGroup>
            {visibleOptions.map((o) => {
              const isSelected = selectedValues.has(o.value)
              return (
                <CommandItem
                  key={o.value}
                  value={`opt:${o.value}`}
                  className="cursor-pointer [&>svg:last-child]:hidden"
                  onSelect={() => toggleValue(o.value)}
                >
                  {/* Presentational only: selection toggles via onSelect, so the
                      checkbox must not handle clicks itself (double-toggle). */}
                  <Checkbox checked={isSelected} tabIndex={-1} className="pointer-events-none" />
                  <span className="truncate">{o.label}</span>
                  {/* Derived options carry a merged `count`; static options read
                      the live facet count by their raw value. */}
                  {(o.count ?? facets?.get(o.value)) !== undefined && (
                    <span className="text-muted-foreground ml-auto pl-2 font-mono text-xs">
                      {o.count ?? facets?.get(o.value)}
                    </span>
                  )}
                </CommandItem>
              )
            })}
          </CommandGroup>
        )}
        <CommandSeparator />
        <CommandGroup>
          {visibleOptions.length > 0 && (
            <>
              <CommandItem
                value="__action__:select-all"
                className="cursor-pointer [&>svg:last-child]:hidden"
                onSelect={selectAllVisible}
              >
                <CheckCheckIcon />
                {t("table.selectAll")}
              </CommandItem>
              <CommandItem
                value="__action__:invert"
                className="cursor-pointer [&>svg:last-child]:hidden"
                onSelect={invertVisible}
              >
                <ArrowLeftRightIcon />
                {t("table.invertSelection")}
              </CommandItem>
            </>
          )}
          {selectedValues.size > 0 && (
            <CommandItem
              value="__action__:clear"
              className="cursor-pointer [&>svg:last-child]:hidden"
              onSelect={() => column.setFilterValue(undefined)}
            >
              <XIcon />
              {t("table.clearFilter")}
            </CommandItem>
          )}
        </CommandGroup>
      </CommandList>
    </Command>
  )
}
