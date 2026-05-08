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

import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Badge } from "@/components/ui/badge"
import { authAtom, clearSessionData, impersonationAtom, isAdminAtom } from "@/lib/atoms"
import { LogOut, Shield, User } from "lucide-react"
import { useMemo } from "react"
import { useTranslation } from "@/lib/i18n"
import { LocaleSwitcher } from "@/components/locale-switcher"
import { loginPath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"

export default function GeneralPage() {
  const router = useRouter()
  const { t } = useTranslation()
  const locale = useLocale()
  const auth = useAtomValue(authAtom)
  const isAdmin = useAtomValue(isAdminAtom)
  const impersonation = useAtomValue(impersonationAtom)

  const user = useMemo(() => {
    if (auth?.role === "admin") {
      return impersonation?.user ?? auth?.user
    }
    return auth?.user
  }, [auth?.role, auth?.user, impersonation?.user])

  const team = useMemo(() => {
    if (auth?.role === "admin") {
      return impersonation?.team ?? auth?.team
    }
    return auth?.team
  }, [auth?.role, auth?.team, impersonation?.team])

  const handleLogout = () => {
    clearSessionData()
    router.push(loginPath(locale))
  }

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <PageHeader title={t("general.title")} />

      <div className="max-w-2xl p-6">
        <div className="flex flex-col gap-6">
          {/* Account Info */}
          <div>
            <h3 className="text-foreground mb-1 font-mono text-sm font-bold tracking-wide uppercase">
              {t("general.account")}
            </h3>
            <p className="text-muted-foreground mb-3 text-xs">{t("general.currentSession")}</p>
            <div className="flex flex-col gap-2">
              <div className="border-border bg-secondary flex items-center gap-3 border px-3 py-2.5">
                <div className="border-border bg-background text-foreground flex h-8 w-8 items-center justify-center border text-sm font-bold">
                  {isAdmin ? (
                    <Shield className="text-brand h-4 w-4" />
                  ) : (
                    <User className="text-muted-foreground h-4 w-4" />
                  )}
                </div>
                <div className="flex flex-col gap-0.5">
                  <div className="flex items-center gap-2">
                    <span className="text-foreground font-mono text-sm font-semibold">
                      {user ?? "Unknown"}
                    </span>
                    <Badge
                      variant={isAdmin ? "outline" : "default"}
                      className="font-mono text-xs uppercase"
                    >
                      {isAdmin ? t("status.admin") : t("status.tenant")}
                    </Badge>
                  </div>
                  {auth?.team && (
                    <span className="text-muted-foreground font-mono text-xs">Team: {team}</span>
                  )}
                </div>
              </div>
            </div>
          </div>

          <Separator />

          {/* Language */}
          <LocaleSwitcher />

          <Separator />

          {/* Sign Out */}
          <div>
            <h3 className="text-foreground mb-1 font-mono text-sm font-bold tracking-wide uppercase">
              {t("general.session")}
            </h3>
            <p className="text-muted-foreground mb-3 text-xs">{t("general.signOutDesc")}</p>
            <Button
              variant="outline"
              onClick={handleLogout}
              className="border-border gap-2 font-mono text-xs tracking-wider uppercase"
            >
              <LogOut className="h-3.5 w-3.5" />
              {t("general.signOut")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
