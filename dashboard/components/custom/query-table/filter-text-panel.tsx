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
import { Column, Row } from "@tanstack/react-table"
import { SearchIcon, SearchXIcon, XIcon } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"

import { useTranslation } from "@/lib/i18n"

import { DataTableFacetedFilterTextProps } from "./faceted-filter"

// A text filter value is a `string[]` of search terms so it round-trips through
// the existing searchParams machinery untouched. Each term is either an
// "include" term (the cell must contain it) or an "exclude" term, marked by a
// leading `!` (the cell must NOT contain it). Terms are AND-combined. Valid
// identifiers (sandbox ids, pool names) never start with `!`, so the prefix is
// an unambiguous sentinel. The panel below edits a single term, but the matcher
// tolerates several for forward compatibility.
export const EXCLUDE_PREFIX = "!"

/** Coerce a stored filter value into a list of non-empty term strings. */
function normalizeTerms(value: unknown): string[] {
  if (value === undefined || value === null) {
    return []
  }
  const arr = Array.isArray(value) ? value : [value]
  return arr
    .map((v) => (v === undefined || v === null ? "" : String(v)))
    .filter((v) => v !== "")
}

/** Read the first term's text and mode (used to seed the panel + chip). */
export function parseTextTerm(value: unknown): {
  text: string
  exclude: boolean
} {
  const first = normalizeTerms(value)[0] ?? ""
  if (first.startsWith(EXCLUDE_PREFIX)) {
    return { text: first.slice(EXCLUDE_PREFIX.length), exclude: true }
  }
  return { text: first, exclude: false }
}

/**
 * Human-readable summary for chips / the dimension list: `~ foo` for include,
 * `≠ foo` for exclude. Returns null when no term is set.
 */
export function formatTextFilter(value: unknown): string | null {
  const terms = normalizeTerms(value)
  if (terms.length === 0) {
    return null
  }
  return terms
    .map((term) =>
      term.startsWith(EXCLUDE_PREFIX)
        ? `≠ ${term.slice(EXCLUDE_PREFIX.length)}`
        : `~ ${term}`,
    )
    .join(", ")
}

/**
 * Include terms only, with the prefix stripped. Excluded terms never match
 * surviving rows, so callers that highlight matches must not highlight them.
 */
export function textFilterIncludeTerms(value: unknown): string[] {
  return normalizeTerms(value).filter((term) => !term.startsWith(EXCLUDE_PREFIX))
}

/**
 * Core matcher shared by the builtin `textFilterFn` and by columns whose cell
 * is a collection. A row passes when every include term matches some haystack
 * and no exclude term matches any haystack.
 */
export function textMatches(haystacks: string[], value: unknown): boolean {
  const terms = normalizeTerms(value)
  if (terms.length === 0) {
    return true
  }
  const lowered = haystacks.map((h) => h.toLowerCase())
  return terms.every((term) => {
    const exclude = term.startsWith(EXCLUDE_PREFIX)
    const needle = (exclude ? term.slice(EXCLUDE_PREFIX.length) : term).toLowerCase()
    if (needle === "") {
      return true
    }
    const matchesSome = lowered.some((h) => h.includes(needle))
    return exclude ? !matchesSome : matchesSome
  })
}

/**
 * Drop-in replacement for the builtin `includesString` that honors exclude.
 * Generic (not `FilterFn<unknown>`) so it assigns to any column's `filterFn`
 * without variance errors.
 */
export function textFilterFn<TData>(row: Row<TData>, columnId: string, value: unknown): boolean {
  return textMatches([String(row.getValue(columnId) ?? "")], value)
}

interface FilterTextPanelProps<TData> {
  column: Column<TData>
  option: DataTableFacetedFilterTextProps
  onApplied: () => void
  // focus the input on mount (skip in the hover-opened flyout, where stealing
  // focus would interrupt typing in the dimension search)
  autoFocusInput?: boolean
}

/**
 * Single-term text editor for one `text` filter dimension: an input plus a
 * Contains / Excludes toggle. Used both as the second-level pane of the Filters
 * menu and inside an active-filter chip popover.
 */
export function FilterTextPanel<TData>({
  column,
  option,
  onApplied,
  autoFocusInput = false,
}: FilterTextPanelProps<TData>) {
  const { t } = useTranslation()
  const current = parseTextTerm(column.getFilterValue())
  const [text, setText] = useState(current.text)
  const [exclude, setExclude] = useState(current.exclude)

  const hasFilter = formatTextFilter(column.getFilterValue()) !== null

  const apply = (nextText: string, nextExclude: boolean) => {
    const trimmed = nextText.trim()
    const nextValue = trimmed
      ? [nextExclude ? `${EXCLUDE_PREFIX}${trimmed}` : trimmed]
      : undefined

    const currentValue = column.getFilterValue()
    const sameValue =
      JSON.stringify(currentValue ?? null) === JSON.stringify(nextValue ?? null)
    if (sameValue) {
      toast.warning(t("table.filterUnchanged"))
      return
    }

    column.setFilterValue(nextValue)
    onApplied()
  }

  const handleClear = () => {
    setText("")
    column.setFilterValue(undefined)
    onApplied()
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      apply(text, exclude)
    }
  }

  return (
    <div className="p-1">
      <div className="truncate px-2 pt-1.5 text-sm font-medium">
        {option.title ?? option.columnKey}
      </div>
      <div className="space-y-2 p-2">
        <Tabs
          value={exclude ? "excludes" : "contains"}
          onValueChange={(value) => setExclude(value === "excludes")}
        >
          <TabsList className="w-full">
            <TabsTrigger value="contains" className="flex-1 text-xs">
              <SearchIcon className="size-3.5" />
              {t("table.textContains")}
            </TabsTrigger>
            <TabsTrigger value="excludes" className="flex-1 text-xs">
              <SearchXIcon className="size-3.5" />
              {t("table.textExcludes")}
            </TabsTrigger>
          </TabsList>
        </Tabs>
        <Input
          type="text"
          autoFocus={autoFocusInput}
          placeholder={
            option.placeholder ?? t("table.searchField", { field: option.title ?? option.columnKey })
          }
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          className="h-8 text-sm"
        />
        <div className="flex items-center justify-center gap-2 pt-1">
          <Button size="sm" className="flex-1" onClick={() => apply(text, exclude)}>
            {t("table.apply")}
          </Button>
          {hasFilter && (
            <Button size="sm" variant="outline" className="flex-1" onClick={handleClear}>
              <XIcon className="size-4" />
              {t("table.clear")}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
