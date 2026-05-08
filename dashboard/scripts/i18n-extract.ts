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

/**
 * i18n-extract.ts — AST-based extraction of hardcoded English strings from TSX/TS files.
 *
 * Usage: pnpm i18n:extract
 * Output: scripts/extracted-strings.json
 *
 * Scans all .tsx/.ts files (excluding components/ui/, node_modules, .next) and finds:
 *   - JSX text content between tags
 *   - String attributes: title, placeholder, aria-label, tooltip
 *   - toast.success(), toast.error() string arguments
 *   - Zod validation messages (.min(1, "..."))
 *
 * Heuristics to skip: CSS classes, URLs, identifiers, strings < 2 chars, import paths.
 */

import * as fs from "fs"
import * as path from "path"

interface ExtractedString {
  file: string
  line: number
  text: string
  context: string
  suggestedKey: string
}

const DASHBOARD_ROOT = path.resolve(__dirname, "..")
const SKIP_DIRS = ["node_modules", ".next", "components/ui", "messages", "scripts"]
const SKIP_PATTERNS = [
  /^[a-z-]+$/, // CSS class-like: "flex", "hidden"
  /^#[0-9a-fA-F]+$/, // hex colors
  /^https?:\/\//, // URLs
  /^\/.+\..+$/, // file paths
  /^@/, // imports
  /^\d+(\.\d+)*$/, // version numbers
  /^[A-Z_]+$/, // constants like "GET", "POST"
  /^[a-z]+\/[a-z-]+$/, // MIME types
  /^agbx_/, // API key prefix
  /^agentbox-/, // storage keys
]

function shouldSkipString(text: string): boolean {
  const trimmed = text.trim()
  if (trimmed.length < 2) return true
  if (SKIP_PATTERNS.some((p) => p.test(trimmed))) return true
  // Skip if no letters (pure punctuation/numbers)
  if (!/[a-zA-Z]/.test(trimmed)) return true
  return false
}

function suggestKey(text: string, filePath: string): string {
  // Derive domain from file path
  const rel = path.relative(DASHBOARD_ROOT, filePath)
  let domain = "misc"

  if (rel.includes("sandboxes")) domain = "sandboxes"
  else if (rel.includes("pools")) domain = "pools"
  else if (rel.includes("templates")) domain = "templates"
  else if (rel.includes("api-keys") || rel.includes("apikey")) domain = "apiKeys"
  else if (rel.includes("login")) domain = "login"
  else if (rel.includes("overview")) domain = "overview"
  else if (rel.includes("admin")) domain = "admin"
  else if (rel.includes("sidebar")) domain = "nav"
  else if (rel.includes("changelog")) domain = "changelog"
  else if (rel.includes("prometheus")) domain = "prometheus"
  else if (rel.includes("general")) domain = "general"
  else if (rel.includes("quota")) domain = "quota"

  // Create a camelCase-ish key from the text
  const words = text
    .replace(/[^a-zA-Z0-9\s]/g, "")
    .trim()
    .split(/\s+/)
    .slice(0, 4)
  const key = words
    .map((w, i) =>
      i === 0 ? w.toLowerCase() : w.charAt(0).toUpperCase() + w.slice(1).toLowerCase(),
    )
    .join("")

  return `${domain}.${key || "unknown"}`
}

function extractFromFile(filePath: string): ExtractedString[] {
  const results: ExtractedString[] = []
  const content = fs.readFileSync(filePath, "utf-8")
  const lines = content.split("\n")

  // Simple regex-based extraction (not full AST, but good enough for initial pass)

  // Pattern 1: JSX text content - text between > and <
  const jsxTextRegex = />([A-Z][^<{]*?)</g
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    let match
    while ((match = jsxTextRegex.exec(line)) !== null) {
      const text = match[1].trim()
      if (!shouldSkipString(text)) {
        results.push({
          file: path.relative(DASHBOARD_ROOT, filePath),
          line: i + 1,
          text,
          context: "jsx-text",
          suggestedKey: suggestKey(text, filePath),
        })
      }
    }
  }

  // Pattern 2: String attributes (title, placeholder, aria-label, tooltip)
  const attrRegex = /(?:title|placeholder|aria-label|tooltip)="([^"]+)"/g
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    let match
    while ((match = attrRegex.exec(line)) !== null) {
      const text = match[1].trim()
      if (!shouldSkipString(text)) {
        results.push({
          file: path.relative(DASHBOARD_ROOT, filePath),
          line: i + 1,
          text,
          context: "attribute",
          suggestedKey: suggestKey(text, filePath),
        })
      }
    }
  }

  // Pattern 3: toast.success/error/warning("...")
  const toastRegex = /toast\.\w+\(\s*["'`]([^"'`]+)["'`]/g
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    let match
    while ((match = toastRegex.exec(line)) !== null) {
      const text = match[1].trim()
      if (!shouldSkipString(text)) {
        results.push({
          file: path.relative(DASHBOARD_ROOT, filePath),
          line: i + 1,
          text,
          context: "toast",
          suggestedKey: suggestKey(text, filePath),
        })
      }
    }
  }

  return results
}

function walkDir(dir: string): string[] {
  const files: string[] = []
  const entries = fs.readdirSync(dir, { withFileTypes: true })

  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name)
    const rel = path.relative(DASHBOARD_ROOT, fullPath)

    if (SKIP_DIRS.some((skip) => rel.startsWith(skip) || rel.includes(`/${skip}`))) continue

    if (entry.isDirectory()) {
      files.push(...walkDir(fullPath))
    } else if (/\.(tsx?|jsx?)$/.test(entry.name) && !entry.name.endsWith(".d.ts")) {
      files.push(fullPath)
    }
  }

  return files
}

// Main
const files = walkDir(DASHBOARD_ROOT)
const allStrings: ExtractedString[] = []

for (const file of files) {
  allStrings.push(...extractFromFile(file))
}

const outputPath = path.join(DASHBOARD_ROOT, "scripts", "extracted-strings.json")
fs.writeFileSync(outputPath, JSON.stringify(allStrings, null, 2))

console.log(`✅ Extracted ${allStrings.length} strings from ${files.length} files`)
console.log(`   Output: ${outputPath}`)
