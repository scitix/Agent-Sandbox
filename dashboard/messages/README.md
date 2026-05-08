# messages/ — Translation Files Directory

This directory contains all i18n translation files. `zh-Hans.json` serves as the **Source of Truth**; all other language files must maintain key consistency with it.

## Directory Structure

```
messages/
├── en.json          ← English translations
├── zh-Hans.json     ← Simplified Chinese (Source of Truth, all keys defined here)
├── zh-Hant.json     ← Traditional Chinese
├── _schema.ts       ← Auto-generated TypeScript types (DO NOT edit manually)
└── README.md        ← This file
```

## Workflow for Adding a New Language

Example: Adding Japanese (`ja`):

### Step 1: Create the Translation File

Copy `zh-Hans.json` to `ja.json` and translate all values:

```bash
cp messages/zh-Hans.json messages/ja.json
```

### Step 2: Register the Locale

Edit `lib/i18n/config.ts` and add the new language to the `locales` array:

```ts
export const I18N_CONFIG = {
  defaultLocale: "en",
  locales: ["en", "zh-Hans", "zh-Hant", "ja"], // ← Add "ja"
  // ...
} as const
```

### Step 3: Preload the Dictionary

Edit `lib/i18n/dictionary.ts` to add a synchronous import and cache seed:

```ts
import jaDict from "@/messages/ja.json"

cache.set("ja", jaDict as TranslationKeys)
```

### Step 4: Register Proxy Routes

Edit `proxy.ts` and add the new locale to both arrays:

```ts
const SUPPORTED_LOCALES = ["zh-Hant", "zh-Hans", "ja"] // ← Add "ja"
const ALL_LOCALES = ["en", "zh-Hans", "zh-Hant", "ja"] // ← Add "ja"
```

To support `Accept-Language` auto-detection, add mapping rules in the `detectLocaleFromHeader` function:

```ts
// Example: Japanese variants
if (lang === "ja" || lang === "ja-jp") return "ja"
```

### Step 5: Add UI Label

Edit `components/locale-switcher.tsx` and add the label to `LOCALE_LABELS`:

```ts
const LOCALE_LABELS: Record<Locale, string> = {
  en: "English",
  "zh-Hans": "简体中文",
  "zh-Hant": "繁體中文",
  ja: "日本語", // ← Add "ja"
}
```

### Step 6: Verification

```bash
pnpm i18n:gen-types    # Regenerate TypeScript types
pnpm i18n:validate     # Validate key consistency across all files
pnpm exec tsc --noEmit # Run TypeScript compilation check
```

## Command Reference

| Command               | Description                                                                          |
| --------------------- | ------------------------------------------------------------------------------------ |
| `pnpm i18n:gen-types` | Generates `_schema.ts` (TS types) from `zh-Hans.json`                                |
| `pnpm i18n:translate` | AI-powered translation for missing keys (requires `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`) |
| `pnpm i18n:validate`  | Validates key consistency and placeholder matching against `zh-Hans.json`             |
| `pnpm i18n:extract`   | Scans code for hardcoded strings (Development helper)                                |

## Key Naming Convention

Use a flat, dot-namespaced format: `"module.category.specific_name"`

```
nav.overview          → Navigation label
common.create         → General action button
sandboxes.col.status  → Sandbox table column header
pools.form.replicas   → Resource pool form field
login.oidc.failed     → OIDC login error
```

## Important Notes

* **`_schema.ts` must not be modified manually**; it is auto-generated via `pnpm i18n:gen-types`.
* **`zh-Hans.json` is the sole source for all keys**. New copy must be added here first.
* **Interpolation Syntax**: Use `{{variableName}}`, e.g., `"Sandbox {{id}} deleted"`.
* **Key Consistency**: All language files must have identical keys. `pnpm i18n:validate` automatically detects and checks all `*.json` files in the directory.