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

import { CalendarIcon } from "lucide-react"
import { format } from "date-fns"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { useTranslation } from "@/lib/i18n"

interface ExpiresAtPickerProps {
  value: Date | undefined
  onChange: (date: Date | undefined) => void
}

export function ExpiresAtPicker({ value, onChange }: ExpiresAtPickerProps) {
  const { t } = useTranslation()
  const today = new Date()
  const maxDate = new Date(today.getFullYear() + 3, 11, 31)

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className="border-border bg-background h-9 w-full justify-start font-mono text-sm font-normal"
          >
            <CalendarIcon className="text-muted-foreground mr-2 h-3.5 w-3.5" />
            {value ? (
              format(value, "PPP")
            ) : (
              <span className="text-muted-foreground">{t("apiKeys.form.pickADate")}</span>
            )}
          </Button>
        }
      />
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={value}
          captionLayout="dropdown"
          onSelect={onChange}
          disabled={(date) => date < today || date > maxDate}
          startMonth={today}
          endMonth={maxDate}
        />
      </PopoverContent>
    </Popover>
  )
}
