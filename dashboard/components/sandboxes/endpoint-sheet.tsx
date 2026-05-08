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

import { useState } from "react"
import { Copy, CheckCheck, Terminal, KeyRound } from "lucide-react"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Button } from "@/components/ui/button"
import { type AgentSandbox } from "@/lib/api/client"
import { CopyButton } from "../custom/button/copy-button"
import { useQuery } from "@tanstack/react-query"
import { globalApiKeysQueryOptions } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

function CodeBlock({ code, language }: { code: string; language: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className="group relative">
      <div className="border-border bg-secondary flex items-center justify-between border px-3 py-1.5">
        <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
          {language}
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground h-6 w-6 opacity-0 transition-opacity group-hover:opacity-100"
          onClick={handleCopy}
        >
          {copied ? (
            <CheckCheck className="h-3 w-3 text-green-500" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
        </Button>
      </div>
      <pre className="border-border bg-background text-foreground overflow-x-auto border border-t-0 p-4 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">
        {code}
      </pre>
    </div>
  )
}

interface EndpointSheetProps {
  sandbox: AgentSandbox | null
  onOpenChange: (open: boolean) => void
}

function EndpointSheetContent({ sandbox, onOpenChange }: EndpointSheetProps) {
  const { t } = useTranslation()
  const { data: apiKeys } = useQuery(globalApiKeysQueryOptions())
  const apiKey = apiKeys?.[0]?.keyId ?? "<your-api-key>"

  if (!sandbox) return null

  const endpoints = sandbox.endpoints ?? {}
  const endpointEntries = Object.entries(endpoints)
  const hasEndpoints = endpointEntries.length > 0

  const firstUrl = hasEndpoints ? endpointEntries[0][1].url : `<endpoint-url>`
  const sandboxId = sandbox.sandboxId

  const curlCode =
    endpointEntries.length > 0
      ? endpointEntries
        .map(
          ([name, ep]) =>
            `# ${name}\ncurl -X GET "${ep.url}" \\\n  -H "AGENTBOX-API-KEY: ${apiKey}"`,
        )
        .join("\n\n")
      : `curl -X GET "<endpoint-url>" \\\n  -H "AGENTBOX-API-KEY: ${apiKey}"`

  const pythonCode = `import requests

API_KEY = "${apiKey}"
SANDBOX_ID = "${sandboxId}"

headers = {
    "AGENTBOX-API-KEY": API_KEY,
}

${endpointEntries.length > 0
      ? endpointEntries
        .map(
          ([name, ep]) =>
            `# ${name}\nresponse = requests.get(\n    "${ep.url}",\n    headers=headers,\n)\nprint(response.text)`,
        )
        .join("\n\n")
      : `response = requests.get(\n    "${firstUrl}",\n    headers=headers,\n)\nprint(response.text)`
    }`

  const tsCode = `const API_KEY = "${apiKey}";
const SANDBOX_ID = "${sandboxId}";

const headers = {
  "AGENTBOX-API-KEY": API_KEY,
};

${endpointEntries.length > 0
      ? endpointEntries
        .map(
          ([name, ep]) =>
            `// ${name}\nconst ${name}Response = await fetch("${ep.url}", { headers });\nconsole.log(await ${name}Response.text());`,
        )
        .join("\n\n")
      : `const response = await fetch("${firstUrl}", { headers });\nconsole.log(await response.text());`
    }`

  return (
    <Sheet open={!!sandbox} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-xl data-[side=right]:sm:max-w-3xl"
      >
        <SheetHeader className="border-border border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
            <Terminal className="text-brand h-4 w-4" />
            {t("sandboxes.endpoints")}
          </SheetTitle>
          <SheetDescription className="text-muted-foreground font-mono text-xs">
            {t("sandboxes.sandboxLogs", { sandboxId })}
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-6 py-4">
          {/* API Key indicator */}
          <div className="border-border bg-secondary flex items-center gap-2 border px-3 py-2">
            <KeyRound className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
            <span className="text-muted-foreground shrink-0 font-mono text-xs font-bold uppercase">
              {t("sandboxes.apiKey")}
            </span>
            <code className="text-foreground flex-1 truncate font-mono text-xs">
              {apiKeys && apiKeys.length > 0 ? apiKey : t("sandboxes.noApiKey")}
            </code>
            {apiKeys && apiKeys.length > 0 && <CopyButton text={apiKey} />}
          </div>

          {/* Endpoint URL list */}
          {hasEndpoints && (
            <div className="flex flex-col gap-2">
              <p className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("sandboxes.availableEndpoints")}
              </p>
              {endpointEntries.map(([name, ep]) => (
                <div
                  key={name}
                  className="border-border bg-secondary flex items-center justify-between gap-2 border px-3 py-2"
                >
                  <span className="text-muted-foreground shrink-0 font-mono text-xs font-bold uppercase">
                    {name}
                  </span>
                  <span className="text-foreground flex-1 truncate text-right font-mono text-xs">
                    {ep.url}
                  </span>
                  <CopyButton text={ep.url} />
                </div>
              ))}
            </div>
          )}

          {!hasEndpoints && (
            <div className="border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
              <p className="font-mono text-xs text-amber-700 dark:text-amber-400">
                {t("sandboxes.noEndpointsAvailable")}
              </p>
            </div>
          )}

          {/* Code examples */}
          <div className="flex flex-col gap-2">
            <p className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("sandboxes.codeExamples")}
            </p>
            <div>
              <Tabs defaultValue="curl" className="flex w-full flex-col">
                <TabsList>
                  <TabsTrigger value="curl" className="h-6 px-3 font-mono text-xs uppercase">
                    {t("sandboxes.curl")}
                  </TabsTrigger>
                  <TabsTrigger value="python" className="h-6 px-3 font-mono text-xs uppercase">
                    {t("sandboxes.python")}
                  </TabsTrigger>
                  <TabsTrigger value="typescript" className="h-6 px-3 font-mono text-xs uppercase">
                    {t("sandboxes.typescript")}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="curl">
                  <CodeBlock code={curlCode} language="bash" />
                </TabsContent>
                <TabsContent value="python">
                  <CodeBlock code={pythonCode} language="python" />
                </TabsContent>
                <TabsContent value="typescript">
                  <CodeBlock code={tsCode} language="typescript" />
                </TabsContent>
              </Tabs>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

export function EndpointSheet({ sandbox, onOpenChange }: EndpointSheetProps) {
  if (!sandbox) return null
  return <EndpointSheetContent sandbox={sandbox} onOpenChange={onOpenChange} />
}
