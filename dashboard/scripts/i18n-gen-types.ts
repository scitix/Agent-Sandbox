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
 * i18n-gen-types.ts — Generate TypeScript types from en.json
 *
 * Usage: pnpm i18n:gen-types
 *
 * Reads messages/en.json and writes messages/_schema.ts with:
 *   - TranslationKey type (union of all keys)
 *   - TranslationKeys record type
 */

import * as fs from "fs"
import * as path from "path"

const MESSAGES_DIR = path.resolve(__dirname, "..", "messages")
const EN_PATH = path.join(MESSAGES_DIR, "en.json")
const SCHEMA_PATH = path.join(MESSAGES_DIR, "_schema.ts")

function main() {
  // Verify en.json exists and is valid JSON
  const content = fs.readFileSync(EN_PATH, "utf-8")
  const en = JSON.parse(content)
  const keyCount = Object.keys(en).length

  const output = `// AUTO-GENERATED — do not edit manually.
// Run: pnpm run i18n:gen-types

import type en from "./en.json"

/** All valid translation keys (derived from en.json) */
export type TranslationKey = keyof typeof en

/** Full dictionary type */
export type TranslationKeys = Record<TranslationKey, string>
`

  fs.writeFileSync(SCHEMA_PATH, output)
  console.log(`✅ Generated ${SCHEMA_PATH} with ${keyCount} keys`)
}

main()
