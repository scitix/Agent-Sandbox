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

import { useState, useMemo, useEffect, useRef, Suspense } from "react"
import { useRouter } from "next/navigation"
import { useQueryState, parseAsString } from "nuqs"
import { useAtomValue, useSetAtom } from "jotai"
import type { AuthState } from "@/lib/atoms"
import { authAtom, clustersAtom } from "@/lib/atoms"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Eye, EyeOff, Loader2, KeyRound, Building2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Field, FieldLabel, FieldError } from "@/components/ui/field"
import { useLogin, useMockLogin, oidcConfigQueryOptions } from "@/lib/queries"
import { useQuery } from "@tanstack/react-query"
import AgentBoxIcon from "@/components/icons/agentbox-icon"
import { clusterPath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/messages/_schema"

const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

// ─── OIDC error message map ───────────────────────────────────────────────────

const OIDC_ERROR_KEYS: Record<string, TranslationKey> = {
  state_mismatch: "login.oidc.stateMismatch",
  no_group: "login.oidc.noGroup",
  oidc_invalid_client: "login.oidc.invalidClient",
  oidc_invalid_grant: "login.oidc.invalidGrant",
  oidc_redirect_uri_mismatch: "login.oidc.redirectUriMismatch",
  oidc_invalid_issuer: "login.oidc.invalidIssuer",
  oidc_missing_claims: "login.oidc.missingClaims",
  oidc_failed: "login.oidc.failed",
}

// ─── Schemas ──────────────────────────────────────────────────────────────────

const apiKeyLoginSchema = z.object({
  apiKey: z.string().min(1, "login.apiKeyRequired"),
})

const mockLoginSchema = z.object({
  team: z.string().min(1, "login.teamRequired"),
  username: z.string().min(1, "login.usernameRequired"),
})

type ApiKeyLoginForm = z.infer<typeof apiKeyLoginSchema>
type MockLoginForm = z.infer<typeof mockLoginSchema>

// ─── Main LoginForm ───────────────────────────────────────────────────────────

function LoginForm() {
  const router = useRouter()
  const setAuth = useSetAtom(authAtom)
  const clustersData = useAtomValue(clustersAtom)
  const locale = useLocale()
  const { t } = useTranslation()
  const [showKey, setShowKey] = useState(false)
  const [apiKeyError, setApiKeyError] = useState<string | null>(null)
  const [ssoError, setSsoError] = useState<string | null>(null)

  // Read error from URL query (set by OIDC callback redirect)
  const [errorParam] = useQueryState(
    "error",
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const urlErrorMessage = errorParam ? t(OIDC_ERROR_KEYS[errorParam] ?? "login.oidc.failed") : null

  // OIDC config — tells us whether to show SSO button or Mock form
  const { data: oidcConfigData } = useQuery(oidcConfigQueryOptions())
  const oidcEnabled = oidcConfigData?.enabled ?? false

  const clusters = useMemo(() => clustersData.clusters, [clustersData.clusters])

  // Type-safe URL query state via nuqs (single source of truth)
  const [clusterParam] = useQueryState(
    "cluster",
    parseAsString.withOptions({ scroll: false, shallow: false }),
  )
  const [redirectParam] = useQueryState(
    "redirect",
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )

  // Resolve the effective cluster ID: validate against available clusters, fall back to first
  const selectedClusterID = useMemo(() => {
    if (clusterParam && clusters.some((c) => c.id === clusterParam)) {
      return clusterParam
    }
    return clusters[0]?.id ?? ""
  }, [clusterParam, clusters])

  const { mutateAsync: triggerLogin, isPending: loadingApiKey } = useLogin()
  const { mutateAsync: triggerMockLogin, isPending: loadingMock } = useMockLogin()

  const apiKeyForm = useForm<ApiKeyLoginForm>({
    resolver: zodResolver(apiKeyLoginSchema),
  })
  const mockForm = useForm<MockLoginForm>({
    resolver: zodResolver(mockLoginSchema),
  })

  const handleAuthSuccess = (authState: AuthState | null | undefined) => {
    if (!authState) return
    setAuth(authState)
    if (redirectParam && redirectParam.startsWith("/")) {
      router.push(redirectParam)
      return
    }
    const clusterID = selectedClusterID || authState.clusterID || "default"
    if (authState.role === "admin") {
      router.push(clusterPath(clusterID, "sandboxes", locale))
    } else {
      router.push(clusterPath(clusterID, "overview", locale))
    }
  }

  const onApiKeySubmit = async (data: ApiKeyLoginForm) => {
    setApiKeyError(null)
    try {
      // No clusterID: an API key authenticates against every cluster, so the
      // BFF validates against whichever one it likes and the session spans all
      // of them. A ?cluster= deep link still pins the post-login landing page.
      const authState = await triggerLogin({ apiKey: data.apiKey })
      handleAuthSuccess(authState)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : t("login.invalidApiKey")
      setApiKeyError(msg)
    }
  }

  // Dev-only auto-login: when NEXT_PUBLIC_DEV_API_KEY is baked in (local dev
  // against a remote BFF, headless browser checks), submit it once on mount so
  // the developer lands on the dashboard without pasting the key. Fires a
  // single attempt — an invalid key shows the normal error, no retry loop.
  const devAutoLoginDone = useRef(false)
  const devApiKey = process.env.NEXT_PUBLIC_DEV_API_KEY
  useEffect(() => {
    if (!devApiKey || devAutoLoginDone.current) return
    devAutoLoginDone.current = true
    void onApiKeySubmit({ apiKey: devApiKey })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [devApiKey])

  const onMockSubmit = async (data: MockLoginForm) => {
    setSsoError(null)
    try {
      const authState = await triggerMockLogin({
        username: data.username,
        team: data.team,
      })
      handleAuthSuccess(authState)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : t("login.loginFailed")
      setSsoError(msg)
    }
  }

  // Whether to show any SSO section at all
  const showSsoSection = oidcEnabled || !oidcEnabled // always show; content differs

  return (
    <div className="bg-background relative flex min-h-screen items-center justify-center overflow-hidden">
      {/* Subtle dot grid background */}
      <div
        className="absolute inset-0 opacity-[0.035]"
        style={{
          backgroundImage:
            "linear-gradient(var(--foreground) 1px, transparent 1px), linear-gradient(90deg, var(--foreground) 1px, transparent 1px)",
          backgroundSize: "40px 40px",
        }}
      />
      {/* Subtle radial vignette */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_80%_60%_at_50%_50%,transparent_40%,var(--background)_100%)]" />

      <div className="relative mx-4 w-full max-w-90">
        {/* ── Brand header ── */}
        <div className="mb-10 flex flex-col items-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center">
            <AgentBoxIcon className="text-brand size-12" />
          </div>
          <div className="text-center">
            <h1 className="text-foreground text-lg font-semibold tracking-tight">Agent Sandbox</h1>
            <p className="text-muted-foreground mt-0.5 font-mono text-[11px] tracking-widest uppercase">
              {t("login.subtitle")}
            </p>
          </div>
        </div>

        {/* ── Main card ── */}
        <div className="border-border bg-card overflow-hidden rounded-xl border shadow-sm">
          {/* Card header stripe */}
          <div className="border-border border-b px-6 py-4">
            <p className="text-foreground font-mono text-[11px] font-semibold tracking-[0.14em] uppercase">
              {t("login.signIn")}
            </p>
            <p className="text-muted-foreground mt-0.5 text-xs">{t("login.signInDesc")}</p>
          </div>

          <div className="px-6 py-5">
            {/* ── API Key section ── */}
            <form
              onSubmit={apiKeyForm.handleSubmit(onApiKeySubmit)}
              className="flex flex-col gap-4"
            >
              {/* API Key field */}
              <Field data-invalid={!!apiKeyForm.formState.errors.apiKey}>
                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("login.apiKey")}
                </FieldLabel>
                <div className="relative">
                  <div className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2">
                    <KeyRound className="h-3.5 w-3.5" />
                  </div>
                  <Input
                    {...apiKeyForm.register("apiKey")}
                    type={showKey ? "text" : "password"}
                    placeholder={t("login.apiKeyPlaceholder")}
                    className="border-border bg-background h-9 pr-9 pl-8 font-mono text-sm"
                    autoComplete="current-password"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground absolute top-1/2 right-2 h-6 w-6 -translate-y-1/2"
                    onClick={() => setShowKey(!showKey)}
                    tabIndex={-1}
                  >
                    {showKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </Button>
                </div>
                <FieldError
                  errors={[apiKeyForm.formState.errors.apiKey]}
                  className="font-mono text-xs"
                />
              </Field>

              {apiKeyError && (
                <div className="border-destructive/40 bg-destructive/5 rounded border px-3 py-2">
                  <p className="text-destructive font-mono text-[11px]">{apiKeyError}</p>
                </div>
              )}

              <Button
                type="submit"
                disabled={loadingApiKey}
                className="bg-foreground text-background hover:bg-foreground/85 h-9 w-full font-mono text-xs tracking-wider uppercase disabled:opacity-50"
              >
                {loadingApiKey ? (
                  <>
                    <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                    {t("login.authenticating")}
                  </>
                ) : (
                  t("login.signInWithApiKey")
                )}
              </Button>
            </form>

            {/* ── Divider ── */}
            {showSsoSection && (
              <div className="my-5 flex items-center gap-3">
                <div className="border-border h-px flex-1 border-t" />
                <span className="text-muted-foreground font-mono text-xs tracking-widest uppercase">
                  {t("common.or")}
                </span>
                <div className="border-border h-px flex-1 border-t" />
              </div>
            )}

            {/* ── SSO / Mock section ── */}
            {oidcEnabled ? (
              /* ── OIDC mode ── */
              <div className="flex flex-col gap-3">
                {(ssoError ?? urlErrorMessage) && (
                  <div className="border-destructive/40 bg-destructive/5 rounded border px-3 py-2">
                    <p className="text-destructive font-mono text-[11px]">
                      {ssoError ?? urlErrorMessage}
                    </p>
                  </div>
                )}
                <Button
                  type="button"
                  variant="outline"
                  className="border-border h-9 w-full gap-2 font-mono text-xs tracking-wider uppercase"
                  onClick={() => {
                    const oidcLoginURL = new URL(
                      basePath + "/api/auth/oidc/login",
                      window.location.origin,
                    )
                    if (redirectParam && redirectParam.startsWith("/")) {
                      oidcLoginURL.searchParams.set("redirect", redirectParam)
                    }
                    window.location.href = oidcLoginURL.toString()
                  }}
                >
                  <Building2 className="h-3.5 w-3.5" />
                  {t("login.signInWithSSO")}
                </Button>
              </div>
            ) : (
              /* ── Mock / dev mode ── */
              <form onSubmit={mockForm.handleSubmit(onMockSubmit)} className="flex flex-col gap-4">
                <div className="grid grid-cols-2 gap-3">
                  {/* Team */}
                  <Field data-invalid={!!mockForm.formState.errors.team}>
                    <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                      {t("login.team")}
                    </FieldLabel>
                    <Input
                      {...mockForm.register("team")}
                      type="text"
                      placeholder="engineering"
                      className="border-border bg-background h-9 font-mono text-sm"
                      autoComplete="organization"
                    />
                    <FieldError
                      errors={[mockForm.formState.errors.team]}
                      className="font-mono text-xs"
                    />
                  </Field>
                  {/* Username */}
                  <Field data-invalid={!!mockForm.formState.errors.username}>
                    <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                      {t("login.username")}
                    </FieldLabel>
                    <Input
                      {...mockForm.register("username")}
                      type="text"
                      placeholder="alice"
                      className="border-border bg-background h-9 font-mono text-sm"
                      autoComplete="username"
                    />
                    <FieldError
                      errors={[mockForm.formState.errors.username]}
                      className="font-mono text-xs"
                    />
                  </Field>
                </div>

                {(ssoError ?? urlErrorMessage) && (
                  <div className="border-destructive/40 bg-destructive/5 rounded border px-3 py-2">
                    <p className="text-destructive font-mono text-[11px]">
                      {ssoError ?? urlErrorMessage}
                    </p>
                  </div>
                )}

                <Button
                  type="submit"
                  variant="outline"
                  disabled={loadingMock}
                  className="border-border h-9 w-full gap-2 font-mono text-xs tracking-wider uppercase disabled:opacity-50"
                >
                  {loadingMock ? (
                    <>
                      <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                      {t("login.authenticating")}
                    </>
                  ) : (
                    <>
                      <Building2 className="h-3.5 w-3.5" />
                      {t("login.signInWithOrg")}
                    </>
                  )}
                </Button>
              </form>
            )}
          </div>
        </div>

        {/* ── Footer ── */}
        <p className="text-muted-foreground mt-5 text-center font-mono text-xs tracking-wider">
          © {new Date().getFullYear()} {t("login.footer")}
        </p>
      </div>
    </div>
  )
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  )
}
