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

import { useEffect, useState, useCallback } from "react"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { isAdminAtom } from "@/lib/atoms"
import { navSectionDefs } from "@/components/app-sidebar"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { useTranslation } from "@/lib/i18n"

interface CommandPaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const isAdmin = useAtomValue(isAdminAtom)
  const { t } = useTranslation()

  const navItems = navSectionDefs
    .filter((section) => {
      if (section.adminOnly) return isAdmin
      if (section.tenantOnly) return !isAdmin
      return true
    })
    .flatMap((section) =>
      section.items
        .filter((item) => !(item.page === "overview" && isAdmin))
        .map((item) => ({
          ...item,
          label: t(item.labelKey),
          href: clusterPath(clusterID, item.page, locale),
          group: t(section.groupKey),
        })),
    )

  const groups = [...new Set(navItems.map((i) => i.group))]

  const runCommand = useCallback(
    (href: string) => {
      onOpenChange(false)
      router.push(href)
    },
    [onOpenChange, router],
  )

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("commandPalette.goTo")}
      description={t("commandPalette.navigateToPage")}
    >
      <Command>
        <CommandInput placeholder={t("commandPalette.searchPages")} />
        <CommandList>
          <CommandEmpty>{t("common.noResultsFound")}</CommandEmpty>
          {groups.map((group, idx) => (
            <div key={group}>
              {idx > 0 && <CommandSeparator />}
              <CommandGroup heading={group}>
                {navItems
                  .filter((i) => i.group === group)
                  .map((item) => (
                    <CommandItem
                      key={item.href}
                      value={item.label}
                      onSelect={() => runCommand(item.href)}
                      className="cursor-pointer"
                    >
                      <item.icon className="text-muted-foreground h-4 w-4" />
                      <span>{item.label}</span>
                    </CommandItem>
                  ))}
              </CommandGroup>
            </div>
          ))}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}

export function useCommandPalette() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault()
        setOpen((prev) => !prev)
      }
    }
    document.addEventListener("keydown", handler)
    return () => document.removeEventListener("keydown", handler)
  }, [])

  return { open, setOpen }
}
