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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Provider as JotaiProvider, useSetAtom } from "jotai"
import { NuqsAdapter } from "nuqs/adapters/next/app"
import { clustersAtom, store } from "@/lib/atoms"
import { useCallback, useEffect, useRef, useState } from "react"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { getClusters } from "@/lib/api/client"
import { I18nProvider } from "@/lib/i18n"
import { useTranslation } from "@/lib/i18n"

function JotaiProviderWrapper({ children }: { children: React.ReactNode }) {
  return <JotaiProvider store={store}>{children}</JotaiProvider>
}

function NoClustersError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="bg-background flex min-h-screen items-center justify-center">
      <div className="flex flex-col items-center gap-3">
        <span className="text-foreground font-mono text-sm font-medium">
          {t("auth.noClustersTitle")}
        </span>
        <span className="text-muted-foreground font-mono text-xs">
          {t("auth.noClustersDescription")}
        </span>
        <button
          onClick={onRetry}
          className="text-brand font-mono text-xs underline-offset-4 hover:underline"
        >
          {t("auth.retry")}
        </button>
      </div>
    </div>
  )
}

function BaseProviderWrapper({ children }: { children: React.ReactNode }) {
  const setClustersData = useSetAtom(clustersAtom)
  const [noClusters, setNoClusters] = useState(false)
  const fetchingRef = useRef(false)
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 1000 * 20, // 20 seconds
            retry: false,
          },
        },
      }),
  )

  const fetchClusters = useCallback(() => {
    if (fetchingRef.current) return
    fetchingRef.current = true
    setNoClusters(false)
    getClusters()
      .then((clusters) => {
        setClustersData(clusters)
        if (clusters.clusters.length === 0) {
          setNoClusters(true)
        }
      })
      .finally(() => {
        fetchingRef.current = false
      })
  }, [setClustersData])

  useEffect(() => {
    fetchClusters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (noClusters) {
    return (
      <NoClustersError
        onRetry={() => {
          fetchClusters()
        }}
      />
    )
  }

  return (
    <QueryClientProvider client={queryClient}>
      <NuqsAdapter>
        <ThemeProvider
          attribute="class"
          defaultTheme="light"
          enableSystem
          disableTransitionOnChange
        >
          <TooltipProvider>{children}</TooltipProvider>
          <Toaster position="bottom-right" closeButton={true} />
        </ThemeProvider>
      </NuqsAdapter>
    </QueryClientProvider>
  )
}

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <JotaiProviderWrapper>
      <I18nProvider>
        <BaseProviderWrapper>{children}</BaseProviderWrapper>
      </I18nProvider>
    </JotaiProviderWrapper>
  )
}
