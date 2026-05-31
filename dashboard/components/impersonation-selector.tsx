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

import { useAtom } from "jotai"
import { useQueryClient, useQuery } from "@tanstack/react-query"
import { impersonationTeamAtom, impersonationUserAtom } from "@/lib/atoms"
import { adminTeamsQueryOptions, adminUsersByTeamQueryOptions } from "@/lib/queries"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"

export function ImpersonationSelector() {
  const { t } = useTranslation()
  const [team, setTeam] = useAtom(impersonationTeamAtom)
  const [user, setUser] = useAtom(impersonationUserAtom)
  const qc = useQueryClient()

  const { data: teams = [] } = useQuery(adminTeamsQueryOptions())
  const { data: users = [] } = useQuery(adminUsersByTeamQueryOptions(team || undefined))

  const handleTeamChange = (value: string | null) => {
    setTeam(value ?? "")
  }

  const handleUserChange = (value: string | null) => {
    if (value && team) {
      setUser(value)
    } else {
      setUser("")
    }
    void qc.invalidateQueries()
  }

  return (
    <div>
      <div className="mb-1 flex items-center justify-between px-2">
        <p className="text-muted-foreground font-mono text-xs font-bold tracking-[0.15em] uppercase">
          {t("auth.actAsUser")}
        </p>
      </div>

      <div className="flex gap-1.5">
        {/* Team selector */}
        <div className="min-w-0 flex-1">
          <Combobox value={team || null} onValueChange={handleTeamChange} items={teams}>
            <ComboboxInput
              placeholder={t("auth.selectTeam")}
              className={cn("h-8 font-mono text-xs")}
              showClear
            />
            <ComboboxContent>
              <ComboboxEmpty>{t("auth.noTeamsFound")}</ComboboxEmpty>
              <ComboboxList>
                {(teamItem) => (
                  <ComboboxItem key={teamItem} value={teamItem}>
                    {teamItem}
                  </ComboboxItem>
                )}
              </ComboboxList>
            </ComboboxContent>
          </Combobox>
        </div>

        {/* User selector */}
        <div className="min-w-0 flex-1">
          <Combobox
            value={user || null}
            onValueChange={handleUserChange}
            items={users}
            disabled={!team}
          >
            <ComboboxInput
              placeholder={!team ? t("auth.selectTeamFirst") : t("auth.selectUser")}
              className={cn("h-8 font-mono text-xs")}
              disabled={!team}
              showClear
            />
            <ComboboxContent>
              <ComboboxList>
                {(u) => (
                  <ComboboxItem key={u} value={u}>
                    <span className="font-mono">{u}</span>
                  </ComboboxItem>
                )}
              </ComboboxList>
              <ComboboxEmpty>{t("auth.noUsersFound")}</ComboboxEmpty>
            </ComboboxContent>
          </Combobox>
        </div>
      </div>
    </div>
  )
}
