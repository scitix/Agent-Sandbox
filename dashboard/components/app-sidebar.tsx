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

import { Fragment } from "react"
import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import {
  Box,
  Layers,
  Settings,
  KeyRound,
  Github,
  FileText,
  Sun,
  Moon,
  PanelLeftClose,
  ExternalLink,
  BarChart3,
  LogOut,
  Database,
  ReceiptTextIcon,
  LayoutDashboard,
  HardDrive,
} from "lucide-react"
import { useTheme } from "next-themes"
import { useAtomValue } from "jotai"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
  useSidebar,
} from "@/components/ui/sidebar"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { clearSessionData, isAdminAtom, isActualAdminAtom } from "@/lib/atoms"
import { ClusterSwitcher } from "@/components/cluster-switcher"
import { ImpersonationSelector } from "@/components/impersonation-selector"
import AgentBoxIcon from "./icons/agentbox-icon"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath, loginPath, type DashboardPage } from "@/lib/cluster-path"
import { ChangelogTrigger } from "@/components/changelog/changelog-trigger"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/messages/_schema"

export interface NavItemDef {
  labelKey: TranslationKey
  page: DashboardPage
  icon: React.ComponentType<{ className?: string }>
}

export interface NavSectionDef {
  groupKey: TranslationKey
  adminOnly?: boolean
  tenantOnly?: boolean
  items: NavItemDef[]
}

export const navSectionDefs: NavSectionDef[] = [
  {
    groupKey: "nav.main",
    items: [
      { labelKey: "nav.overview", page: "overview", icon: LayoutDashboard },
      { labelKey: "nav.sandboxes", page: "sandboxes", icon: Box },
      { labelKey: "nav.pools", page: "pools", icon: Database },
    ],
  },
  {
    groupKey: "nav.environment",
    items: [
      { labelKey: "nav.templates", page: "templates", icon: Layers },
      { labelKey: "nav.datasets", page: "datasets", icon: HardDrive },
    ],
  },
  {
    groupKey: "nav.team",
    items: [
      { labelKey: "nav.general", page: "general", icon: Settings },
      { labelKey: "nav.apiKeys", page: "api-keys", icon: KeyRound },
    ],
  },
  {
    groupKey: "nav.billing",
    tenantOnly: true,
    items: [{ labelKey: "nav.quota", page: "quota", icon: ReceiptTextIcon }],
  },
  {
    groupKey: "nav.admin",
    adminOnly: true,
    items: [
      { labelKey: "nav.adminStats", page: "admin", icon: BarChart3 },
      { labelKey: "nav.apiKeys", page: "admin-api-keys", icon: KeyRound },
    ],
  },
]

function NavSection({
  label,
  items,
  clusterID,
}: {
  label?: string
  items: NavItemDef[]
  clusterID: string
}) {
  const pathname = usePathname()
  const { t } = useTranslation()
  const locale = useLocale()

  return (
    <SidebarGroup>
      {label && (
        <SidebarGroupLabel className="text-muted-foreground px-2 text-xs font-bold tracking-[0.15em] uppercase">
          {label}
        </SidebarGroupLabel>
      )}
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => {
            const href = clusterPath(clusterID, item.page, locale)
            const isActive = pathname === href || pathname.startsWith(href + "/")
            const itemLabel = t(item.labelKey)
            return (
              <SidebarMenuItem key={item.labelKey}>
                <SidebarMenuButton
                  render={<Link href={href} />}
                  isActive={isActive}
                  className={
                    isActive
                      ? "text-brand hover:bg-sidebar-accent bg-transparent font-semibold"
                      : ""
                  }
                  tooltip={itemLabel}
                >
                  <item.icon className={isActive ? "text-brand" : ""} />
                  <span>{itemLabel}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            )
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}

interface AppSidebarProps {
  onOpenCommand?: () => void
}

export function AppSidebar({ }: AppSidebarProps) {
  const { theme, setTheme } = useTheme()
  const { state, toggleSidebar, isMobile } = useSidebar()
  const isCollapsed = state === "collapsed"
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const { t } = useTranslation()

  const isAdmin = useAtomValue(isAdminAtom)
  const isActualAdmin = useAtomValue(isActualAdminAtom)

  const handleLogout = () => {
    clearSessionData()
    router.push(loginPath(locale))
  }

  return (
    <Sidebar collapsible="icon" className="border-sidebar-border border-r">
      <SidebarHeader className="px-3 py-3">
        <div className="flex items-center justify-between">
          {/* Logo: when collapsed acts as toggle, when expanded is a Home link */}
          {isCollapsed ? (
            <button
              onClick={toggleSidebar}
              className="flex w-full items-center justify-center gap-2"
              aria-label={t("nav.expandSidebar")}
            >
              <AgentBoxIcon className="text-brand size-6" />
            </button>
          ) : (
            <div className="flex items-center gap-2">
              <AgentBoxIcon className="text-brand size-6" />
              <span className="text-base font-bold tracking-tight">Agent Sandbox</span>
            </div>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={toggleSidebar}
            hidden={isMobile}
            className="text-muted-foreground hover:text-foreground h-6 w-6 group-data-[collapsible=icon]:hidden"
          >
            <PanelLeftClose className="h-4 w-4" />
            <span className="sr-only">{t("nav.toggleSidebar")}</span>
          </Button>
        </div>
        <div className="mt-2 group-data-[collapsible=icon]:hidden">
          {/* Cluster Switcher (multi-cluster mode only) */}
          <ClusterSwitcher />
        </div>
        {isActualAdmin && (
          <div className="mt-2 group-data-[collapsible=icon]:hidden">
            <ImpersonationSelector />
          </div>
        )}
      </SidebarHeader>

      <SidebarSeparator className="mx-0 border-t px-0" />

      <SidebarContent>
        {navSectionDefs
          .filter((section) => {
            if (section.adminOnly) return isAdmin
            if (section.tenantOnly) return !isAdmin
            return true
          })
          .map((section) => (
            <Fragment key={section.groupKey}>
              <NavSection
                label={section.groupKey !== "nav.main" ? t(section.groupKey) : undefined}
                items={section.items}
                clusterID={clusterID}
              />
            </Fragment>
          ))}
      </SidebarContent>

      <SidebarSeparator className="mx-0 border-t px-0" />

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              render={
                <a href="https://github.com/scitix/agent-sandbox" target="_blank" rel="noopener noreferrer" />
              }
              tooltip={t("nav.github")}
            >
              <Github className="h-4 w-4" />
              <span>{t("nav.github")}</span>
              <ExternalLink className="text-muted-foreground ml-auto h-3 w-3" />
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              render={
                <a
                  href="https://scitix.github.io/agent-sandbox/en/"
                  target="_blank"
                  rel="noopener noreferrer"
                />
              }
              tooltip={t("nav.documentation")}
            >
              <FileText className="h-4 w-4" />
              <span>{t("nav.documentation")}</span>
              <ExternalLink className="text-muted-foreground ml-auto h-3 w-3" />
            </SidebarMenuButton>
          </SidebarMenuItem>
          <ChangelogTrigger />
        </SidebarMenu>

        <Separator className="my-1" />

        <div className="flex items-center justify-between px-0.5 group-data-[collapsible=icon]:justify-center">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            className="h-7 w-7"
          >
            <Sun className="h-4 w-4 scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90" />
            <Moon className="absolute h-4 w-4 scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0" />
            <span className="sr-only">{t("nav.toggleTheme")}</span>
          </Button>

          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleLogout}
            className="text-muted-foreground hover:text-destructive h-7 w-7 group-data-[collapsible=icon]:hidden"
            title={t("nav.signOut")}
          >
            <LogOut className="h-4 w-4" />
            <span className="sr-only">{t("nav.signOut")}</span>
          </Button>
        </div>
      </SidebarFooter>
    </Sidebar>
  )
}
