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

/**
 * Unified Markdown renderer used across the dashboard.
 *
 * Code blocks (fenced ``` ``` ) are highlighted with Shiki using a dual-theme
 * setup (github-light / github-dark) that automatically follows the
 * Tailwind `.dark` class.
 *
 * Shiki outputs CSS custom properties (--shiki-light / --shiki-dark) as inline
 * styles; the actual `color` rules live in app/globals.css:
 *   .shiki span { color: var(--shiki-light); }
 *   .dark .shiki span { color: var(--shiki-dark); }
 *
 * Usage:
 *   <MarkdownRenderer content={markdown} />
 *   <MarkdownRenderer content={markdown} compact />   // tighter spacing
 */

import { useEffect, useMemo, useState } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { getSingletonHighlighter } from "shiki"
import { cn } from "@/lib/utils"
import { CopyButton } from "@/components/custom/button/copy-button"

// ---------------------------------------------------------------------------
// Singleton highlighter — loaded once, reused for all blocks.
// Only python and yaml are bundled; unknown langs fall back to plain text.
// ---------------------------------------------------------------------------

const SUPPORTED_LANGS = ["python", "yaml", "bash", "typescript", "javascript"] as const
type SupportedLang = (typeof SUPPORTED_LANGS)[number]

const highlighterPromise = getSingletonHighlighter({
  themes: ["github-light", "github-dark"],
  langs: [...SUPPORTED_LANGS],
})

async function highlight(code: string, lang: string): Promise<string | null> {
  const safeLang: SupportedLang | null = (SUPPORTED_LANGS as readonly string[]).includes(lang)
    ? (lang as SupportedLang)
    : null
  if (!safeLang) return null // unknown language → plain fallback
  try {
    const h = await highlighterPromise
    return h.codeToHtml(code, {
      lang: safeLang,
      themes: { light: "github-light", dark: "github-dark" },
      defaultColor: false,
    })
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// Shiki code block — renders once per code node, swaps plain fallback while
// the async highlight resolves.
// ---------------------------------------------------------------------------

function extractText(node: React.ReactNode): string {
  if (typeof node === "string") return node
  if (typeof node === "number") return String(node)
  if (Array.isArray(node)) return node.map(extractText).join("")
  if (node !== null && typeof node === "object" && "props" in (node as React.ReactElement)) {
    const el = node as React.ReactElement<{ children?: React.ReactNode }>
    return extractText(el.props.children)
  }
  return ""
}

interface ShikiBlockProps {
  code: string
  lang: string
  compact?: boolean
}

function ShikiBlock({ code, lang, compact }: ShikiBlockProps) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    highlight(code, lang)
      .then((result: string | null) => {
        if (!cancelled) setHtml(result)
      })
      .catch(() => {
        if (!cancelled) setHtml(null)
      })
    return () => {
      cancelled = true
    }
  }, [code, lang])

  const wrapper = cn("group/pre relative", compact ? "my-1.5" : "my-2")

  if (!html) {
    return (
      <div className={wrapper}>
        <pre className="bg-secondary min-w-0 overflow-x-auto rounded">
          <code
            className={cn(
              "bg-secondary text-foreground block min-w-0 rounded px-3 py-2 font-mono leading-relaxed whitespace-pre",
              compact ? "text-xs" : "text-xs",
            )}
          >
            {code}
          </code>
        </pre>
        <div className="absolute top-1.5 right-1.5 opacity-0 transition-opacity group-hover/pre:opacity-100">
          <CopyButton text={code.trimEnd()} />
        </div>
      </div>
    )
  }

  return (
    <div className={wrapper}>
      {/*
        Shiki injects inline background colours via CSS vars when defaultColor is false.
        We override the <pre> background so it matches our design-system token.
      */}
      <div
        className={cn(
          "[&_pre]:bg-secondary! [&_pre]:min-w-0 [&_pre]:overflow-x-auto",
          "[&_pre]:rounded [&_pre]:px-3 [&_pre]:py-2 [&_pre]:leading-relaxed",
          compact ? "[&_pre]:text-xs" : "[&_pre]:text-xs",
        )}
        // biome-ignore lint/security/noDangerouslySetInnerHtml: content is from Shiki, not user input
        dangerouslySetInnerHTML={{ __html: html }}
      />
      <div className="absolute top-1.5 right-1.5 opacity-0 transition-opacity group-hover/pre:opacity-100">
        <CopyButton text={code.trimEnd()} />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// MarkdownRenderer
// ---------------------------------------------------------------------------

export interface MarkdownRendererProps {
  content: string
  /**
   * compact: tighter spacing — for dialog/sheet bodies.
   * Default (false): more spacious layout for page-level content.
   */
  compact?: boolean
  className?: string
}

export function MarkdownRenderer({ content, compact = false, className }: MarkdownRendererProps) {
  const components = useMemo(
    () => ({
      h1: ({ children }: { children?: React.ReactNode }) => (
        <h1
          className={cn(
            "text-foreground font-mono font-bold tracking-tight uppercase",
            compact ? "mt-3 mb-1.5 text-base" : "mt-6 mb-2 text-xl",
          )}
        >
          {children}
        </h1>
      ),
      h2: ({ children }: { children?: React.ReactNode }) => (
        <h2
          className={cn(
            "text-foreground font-mono font-semibold tracking-tight",
            compact ? "mt-3 mb-1.5 text-sm" : "mt-5 mb-2 text-base",
          )}
        >
          {children}
        </h2>
      ),
      h3: ({ children }: { children?: React.ReactNode }) => (
        <h3
          className={cn(
            "text-foreground font-semibold",
            compact ? "mt-2 mb-1 text-sm" : "mt-4 mb-1.5 text-sm",
          )}
        >
          {children}
        </h3>
      ),
      p: ({ children }: { children?: React.ReactNode }) => (
        <p
          className={cn(
            "text-foreground/80 leading-relaxed",
            compact ? "mb-1.5 text-xs" : "mb-2 text-sm",
          )}
        >
          {children}
        </p>
      ),
      ul: ({ children }: { children?: React.ReactNode }) => (
        <ul
          className={cn(
            "text-foreground/80 list-disc",
            compact ? "mb-2 ml-4 space-y-0.5 text-xs" : "mb-3 ml-5 space-y-1 text-sm",
          )}
        >
          {children}
        </ul>
      ),
      ol: ({ children }: { children?: React.ReactNode }) => (
        <ol
          className={cn(
            "text-foreground/80 list-decimal",
            compact ? "mb-2 ml-4 space-y-0.5 text-xs" : "mb-3 ml-5 space-y-1 text-sm",
          )}
        >
          {children}
        </ol>
      ),
      li: ({ children }: { children?: React.ReactNode }) => (
        <li className="leading-relaxed">{children}</li>
      ),
      strong: ({ children }: { children?: React.ReactNode }) => (
        <strong className="text-foreground font-semibold">{children}</strong>
      ),
      em: ({ children }: { children?: React.ReactNode }) => (
        <em className="text-foreground/80 italic">{children}</em>
      ),
      code: ({
        children,
        className: codeClassName,
      }: {
        children?: React.ReactNode
        className?: string
      }) => {
        // Fenced block — delegate to Shiki
        if (codeClassName?.startsWith("language-")) {
          const lang = codeClassName.replace("language-", "")
          const code =
            typeof children === "string"
              ? children.replace(/\n$/, "")
              : extractText(children).replace(/\n$/, "")
          return <ShikiBlock code={code} lang={lang} compact={compact} />
        }
        // Inline code
        return (
          <code className="bg-secondary text-brand rounded px-1 py-0.5 font-mono text-xs">
            {children}
          </code>
        )
      },
      // Suppress the default <pre> wrapper — ShikiBlock renders its own
      pre: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
      blockquote: ({ children }: { children?: React.ReactNode }) => (
        <blockquote
          className={cn(
            "border-brand text-muted-foreground border-l-2 pl-3 italic",
            compact ? "my-1.5" : "my-2",
          )}
        >
          {children}
        </blockquote>
      ),
      hr: () => <hr className="border-border my-3" />,
      a: ({ href, children }: { href?: string; children?: React.ReactNode }) => (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-brand underline underline-offset-2 hover:opacity-80"
        >
          {children}
        </a>
      ),
      table: ({ children }: { children?: React.ReactNode }) => (
        <div className={cn("overflow-x-auto", compact ? "my-2" : "my-3")}>
          <table className="border-border w-full border-collapse border text-xs">{children}</table>
        </div>
      ),
      thead: ({ children }: { children?: React.ReactNode }) => (
        <thead className="bg-secondary">{children}</thead>
      ),
      th: ({ children }: { children?: React.ReactNode }) => (
        <th className="border-border text-foreground border px-3 py-1.5 text-left font-semibold">
          {children}
        </th>
      ),
      td: ({ children }: { children?: React.ReactNode }) => (
        <td className="border-border text-foreground/80 border px-3 py-1.5">{children}</td>
      ),
    }),
    [compact],
  )

  return (
    <div className={cn("min-w-0 text-sm", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
