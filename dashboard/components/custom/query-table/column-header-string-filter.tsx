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
import { SearchIcon, XIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useDebouncedCallback } from "use-debounce"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"

interface ColumnHeaderStringFilterProps<TData, TValue> {
  column: Column<TData, TValue>
  title: string
  options: {
    title?: string
    placeholder?: string
  }
  onActiveChange?: (active: boolean) => void
  isVisible?: boolean
}

export function ColumnHeaderStringFilter<TData, TValue>({
  column,
  title,
  options,
  onActiveChange,
  isVisible = true,
}: ColumnHeaderStringFilterProps<TData, TValue>) {
  // Separate boolean controls input visibility; inputValue is always a string (never undefined)
  // so the <Input> stays controlled for its entire lifetime.
  const [isOpen, setIsOpen] = useState<boolean>(() => column?.getFilterValue() !== undefined)
  const [inputValue, setInputValue] = useState<string>(() => {
    const v = column?.getFilterValue()
    return typeof v === "string" ? v : ""
  })
  const inputRef = useRef<HTMLInputElement>(null)

  // Track whether the latest filter change originated from our input (not external).
  // Using state (not ref) so it can be safely read during render.
  const [isLocalUpdate, setIsLocalUpdate] = useState(false)

  // Notify parent whenever open state changes
  useEffect(() => {
    onActiveChange?.(isOpen)
  }, [isOpen, onActiveChange])

  // Debounced column filter update
  const debouncedSetFilter = useDebouncedCallback((value: string) => {
    column?.setFilterValue(value || undefined)
  }, 500)

  // Sync external filter changes (e.g. toolbar "Clear all") to local state during render.
  // This follows the React pattern for adjusting state when derived values change:
  // https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes
  const filterValue = column?.getFilterValue() as string | undefined
  const [prevFilterValue, setPrevFilterValue] = useState(filterValue)
  if (filterValue !== prevFilterValue) {
    setPrevFilterValue(filterValue)
    if (!isLocalUpdate) {
      if (filterValue === undefined) {
        setInputValue("")
        setIsOpen(false)
      } else if (typeof filterValue === "string") {
        setInputValue(filterValue)
        setIsOpen(true)
      }
    }
    // Clear the local-update flag once the filter value has been applied
    setIsLocalUpdate(false)
  }

  const handleInputChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setIsLocalUpdate(true)
    setInputValue(value)
    debouncedSetFilter(value)
  }

  const handleClear = () => {
    debouncedSetFilter.cancel()
    setInputValue("")
    setIsOpen(false)
    column?.setFilterValue(undefined)
  }

  const handleSearchClick = () => {
    setIsOpen(true)
    // Wait for the input to mount before focusing
    setTimeout(() => {
      inputRef.current?.focus()
    }, 0)
  }

  return (
    <>
      <div className="relative ml-1 flex w-32 min-w-0 items-center gap-1" hidden={!isOpen}>
        <Input
          ref={inputRef}
          type="text"
          placeholder={options.placeholder || `Search ${options.title || title}`}
          value={inputValue}
          onChange={handleInputChange}
          className="h-6 w-32 min-w-0 flex-1 pr-8 text-xs md:text-xs"
        />
        <Button
          variant="ghost"
          size="icon"
          className="absolute top-1/2 right-2 size-5 -translate-y-1/2 rounded-full"
          onClick={handleClear}
        >
          <XIcon className="size-3" />
        </Button>
      </div>
      <TooltipButton
        variant="ghost"
        size="icon"
        className={cn(
          "size-6 transition-opacity duration-200",
          !isVisible && "pointer-events-none opacity-0",
        )}
        tooltip={`Search ${options.title || title}`}
        onClick={handleSearchClick}
        hidden={isOpen}
      >
        <SearchIcon className="size-3.5" />
      </TooltipButton>
    </>
  )
}
