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

import { Controller, useWatch, type Control, type FieldValues, type Path } from "react-hook-form"

import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useTranslation } from "@/lib/i18n"

/**
 * The egress-policy fields, shared by the SandboxEnv form and the create-sandbox
 * form. The i18n keys stay under `envs.form.networkPolicy.*` — they describe the
 * policy semantics, not the Env resource, so both callers read the same copy.
 */
export type NetworkPolicyMode = "unrestricted" | "disable" | "allowlist"

/** Field names a host form must expose for these controls to bind to. */
export interface NetworkPolicyFormValues {
  networkPolicyMode: NetworkPolicyMode
  allowedDomains?: string
  allowedCIDRs?: string
  deniedCIDRs?: string
  allowPrivateNetworks?: boolean
}

interface Props<T extends FieldValues> {
  control: Control<T>
  register: (name: Path<T>) => Record<string, unknown>
  /** Per-field validation messages, keyed by field name. Values are i18n keys. */
  errors?: Partial<Record<keyof NetworkPolicyFormValues, { message?: string }>>
  /**
   * Whether to offer the "allow private networks" switch.
   *
   * The create-sandbox form sets this false: E2B's `SandboxNetworkConfig` has no
   * field for it, so a per-sandbox request cannot carry it and the sandbox always
   * runs under the anti-SSRF baseline. Showing a switch that silently does
   * nothing is worse than not showing it — declare it on the SandboxEnv instead.
   */
  showPrivateNetworks?: boolean
  /** Rendered above the fields; omit on forms that already have a section header. */
  heading?: string
}

export function NetworkPolicyFields<T extends FieldValues>({
  control,
  register,
  errors,
  showPrivateNetworks = true,
  heading,
}: Props<T>) {
  const { t } = useTranslation()
  const mode = useWatch({ control, name: "networkPolicyMode" as Path<T> }) as
    | NetworkPolicyMode
    | undefined

  return (
    <section className="space-y-3">
      <div>
        {heading && (
          <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
            {heading}
          </h3>
        )}
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.networkPolicy.hint")}</p>
      </div>

      <Field>
        <FieldLabel>{t("envs.form.networkPolicy.mode")}</FieldLabel>
        <Controller
          control={control}
          name={"networkPolicyMode" as Path<T>}
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="unrestricted">
                  {t("envs.form.networkPolicy.modeUnrestricted")}
                </SelectItem>
                <SelectItem value="disable">{t("envs.form.networkPolicy.modeDisable")}</SelectItem>
                <SelectItem value="allowlist">
                  {t("envs.form.networkPolicy.modeAllowlist")}
                </SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        <FieldDescription>{t("envs.form.networkPolicy.modeDescription")}</FieldDescription>
      </Field>

      {mode === "unrestricted" && (
        <p className="text-muted-foreground rounded-md border border-dashed p-3 text-xs">
          {t("envs.form.networkPolicy.unrestrictedPrivateNote")}
        </p>
      )}

      {mode === "allowlist" && (
        <>
          <Field>
            <FieldLabel htmlFor="np-domains">
              {t("envs.form.networkPolicy.allowedDomains")}
            </FieldLabel>
            <Textarea
              id="np-domains"
              rows={3}
              placeholder={"pypi.org\n*.pythonhosted.org"}
              className="font-mono text-sm"
              {...register("allowedDomains" as Path<T>)}
            />
            {errors?.allowedDomains && (
              <FieldError>{t(errors.allowedDomains.message as never)}</FieldError>
            )}
            <FieldDescription>
              {t("envs.form.networkPolicy.allowedDomainsDescription")}
            </FieldDescription>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="np-allow-cidr">
                {t("envs.form.networkPolicy.allowedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-allow-cidr"
                rows={2}
                placeholder="8.8.8.8/32"
                className="font-mono text-sm"
                {...register("allowedCIDRs" as Path<T>)}
              />
              {errors?.allowedCIDRs && (
                <FieldError>{t(errors.allowedCIDRs.message as never)}</FieldError>
              )}
            </Field>
            <Field>
              <FieldLabel htmlFor="np-deny-cidr">
                {t("envs.form.networkPolicy.deniedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-deny-cidr"
                rows={2}
                placeholder="1.2.3.4/32"
                className="font-mono text-sm"
                {...register("deniedCIDRs" as Path<T>)}
              />
              {errors?.deniedCIDRs && (
                <FieldError>{t(errors.deniedCIDRs.message as never)}</FieldError>
              )}
            </Field>
          </div>
        </>
      )}

      {showPrivateNetworks && mode !== "unrestricted" && (
        <Controller
          control={control}
          name={"allowPrivateNetworks" as Path<T>}
          render={({ field }) => (
            <div className="flex items-center justify-between rounded-md border p-3">
              <div className="space-y-0.5 pr-3">
                <FieldLabel>{t("envs.form.networkPolicy.allowPrivateNetworks")}</FieldLabel>
                <FieldDescription>
                  {t("envs.form.networkPolicy.allowPrivateNetworksDescription")}
                </FieldDescription>
              </div>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </div>
          )}
        />
      )}
    </section>
  )
}
