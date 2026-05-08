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
 * i18n-validate.ts — Validate translation file completeness and consistency.
 *
 * Usage: pnpm i18n:validate
 *
 * Checks ALL locale files (zh-Hans.json, zh-Hant.json, etc.) against en.json:
 *   1. All en.json keys exist in each locale file (and vice versa)
 *   2. {{placeholder}} tokens match between en and each locale
 *   3. No empty string values
 *
 * Exit code 1 if any issues found (CI-friendly).
 */

import * as fs from "fs"
import * as path from "path"

const MESSAGES_DIR = path.resolve(__dirname, "..", "messages")
const EN_PATH = path.join(MESSAGES_DIR, "en.json")

// Auto-discover all locale JSON files (everything except en.json and _schema.ts)
function discoverLocaleFiles(): { locale: string; filePath: string }[] {
  const files = fs.readdirSync(MESSAGES_DIR)
  return files
    .filter((f) => f.endsWith(".json") && f !== "en.json")
    .map((f) => ({
      locale: f.replace(".json", ""),
      filePath: path.join(MESSAGES_DIR, f),
    }))
}

function extractPlaceholders(text: string): string[] {
  const matches = text.match(/\{\{(\w+)\}\}/g)
  return matches ? matches.sort() : []
}

function validateLocale(
  en: Record<string, string>,
  enKeys: Set<string>,
  locale: string,
  filePath: string,
): { errors: string[]; warnings: string[] } {
  const errors: string[] = []
  const warnings: string[] = []
  const dict: Record<string, string> = JSON.parse(fs.readFileSync(filePath, "utf-8"))
  const dictKeys = new Set(Object.keys(dict))

  // Check for missing keys
  for (const key of enKeys) {
    if (!dictKeys.has(key)) {
      errors.push(`❌ Missing in ${locale}.json: "${key}"`)
    }
  }

  // Check for extra keys
  for (const key of dictKeys) {
    if (!enKeys.has(key)) {
      warnings.push(`⚠️  Extra key in ${locale}.json (not in en.json): "${key}"`)
    }
  }

  // Check placeholder consistency
  for (const key of enKeys) {
    if (!dictKeys.has(key)) continue
    const enPlaceholders = extractPlaceholders(en[key])
    const localePlaceholders = extractPlaceholders(dict[key])
    if (JSON.stringify(enPlaceholders) !== JSON.stringify(localePlaceholders)) {
      errors.push(
        `❌ Placeholder mismatch for "${key}" in ${locale}.json: en=${enPlaceholders.join(",")} ${locale}=${localePlaceholders.join(",")}`,
      )
    }
  }

  // Check for empty values
  for (const [key, value] of Object.entries(dict)) {
    if (!value.trim()) {
      errors.push(`❌ Empty value in ${locale}.json: "${key}"`)
    }
  }

  return { errors, warnings }
}

function main() {
  const en: Record<string, string> = JSON.parse(fs.readFileSync(EN_PATH, "utf-8"))
  const enKeys = new Set(Object.keys(en))
  const localeFiles = discoverLocaleFiles()

  // Check en.json itself for empty values
  const enErrors: string[] = []
  for (const [key, value] of Object.entries(en)) {
    if (!value.trim()) {
      enErrors.push(`❌ Empty value in en.json: "${key}"`)
    }
  }

  let totalErrors = enErrors.length
  let totalWarnings = 0

  console.log(`\n📊 i18n Validation Report`)
  console.log(`   en.json: ${enKeys.size} keys (source of truth)`)
  console.log(`   Locales: ${localeFiles.map((f) => f.locale).join(", ")}\n`)

  if (enErrors.length > 0) {
    console.log("en.json errors:")
    enErrors.forEach((e) => console.log(`  ${e}`))
    console.log()
  }

  for (const { locale, filePath } of localeFiles) {
    const dict: Record<string, string> = JSON.parse(fs.readFileSync(filePath, "utf-8"))
    const dictKeyCount = Object.keys(dict).length
    const { errors, warnings } = validateLocale(en, enKeys, locale, filePath)

    console.log(`── ${locale}.json (${dictKeyCount} keys) ──`)

    if (warnings.length > 0) {
      warnings.forEach((w) => console.log(`  ${w}`))
    }
    if (errors.length > 0) {
      errors.forEach((e) => console.log(`  ${e}`))
    }
    if (errors.length === 0 && warnings.length === 0) {
      console.log(`  ✅ OK`)
    }
    console.log()

    totalErrors += errors.length
    totalWarnings += warnings.length
  }

  if (totalErrors > 0) {
    console.log(`❌ ${totalErrors} error(s) found across ${localeFiles.length + 1} files.`)
    process.exit(1)
  } else {
    console.log(`✅ All checks passed! (${totalWarnings} warning(s))`)
  }
}

main()
