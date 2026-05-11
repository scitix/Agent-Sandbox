# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

Static documentation & marketing website for [Agent Sandbox](https://github.com/scitix/agent-sandbox), built with **Next.js 16 + fumadocs** and deployed to **GitHub Pages** at `https://scitix.github.io/Agent-Sandbox/`.

Production uses `output: 'export'` (fully static), basePath `/Agent-Sandbox/`, and unoptimized images. The `next-sitemap` postbuild step generates `sitemap.xml` and `robots.txt` into `out/`.

---

## Commands

```bash
pnpm dev                   # Dev server (localhost:3000, no basePath)
pnpm build                 # Production build → static export + sitemap
pnpm generate              # Regenerate API docs from OpenAPI YAML
pnpm compress-images       # Optimize images via Pillow (needs uv)
```

There are no tests in this sub-project.

---

## Architecture

### Content Sources → Pages

The site has two content pipelines that merge into a single page tree under `/docs`:

| Source | Location | Pipeline | Output |
|--------|----------|----------|--------|
| MDX docs | `content/docs/*.mdx` | fumadocs-mdx | Rendered via `MDX` component |
| OpenAPI spec | `../../pkg/openapi/native/openapi.yaml` | fumadocs-openapi → `pnpm generate` | Auto-generated pages in `content/docs/(api)/` |

Both are unified through `lib/source.ts` using fumadocs `loader()` with the `openapiPlugin`. The docs layout (`app/docs/layout.tsx`) splits them into two tabs: "Docs" and "API Reference".

### OpenAPI Filtering

`lib/openapi.ts` reads the Go project's `openapi.yaml` and strips endpoints tagged `admin` (`HIDDEN_TAGS`). After modifying the upstream OpenAPI spec, run `pnpm generate` to regenerate the API doc pages (preserves `meta.json`).

### Client vs Server Components

- **Home page** (`app/(home)/`): `page.tsx` is a server component with SEO metadata + JSON-LD; it renders `<HomePageClient />` for interactive sections.
- **HeroSection**: `HeroContent.tsx` is a **server component** (semantic h1/description/CTA — SSR-visible to crawlers). `HeroSection.tsx` is a `'use client'` wrapper that lazy-mounts the decorative `HeroReel` via `requestIdleCallback`.
- **Docs pages**: `app/docs/[[...slug]]/page.tsx` is an async server component that branches on `page.type` (openapi vs mdx). The API page client component is in `components/api-page.tsx`.
- **API index** (`app/docs/api/page.tsx`): client-side redirect to `/docs/api/sandboxes/`.

### Asset Path Handling

All public asset references must go through `publicAsset()` from `lib/public-assets.ts`, which prepends `NEXT_PUBLIC_BASE_PATH` (`/Agent-Sandbox` in production, empty in dev). This applies to `<Image src>`, `<img src>`, and any hardcoded path to files in `public/`.

---

## Key Files

| File | Role |
|------|------|
| `source.config.ts` | fumadocs-mdx config — points to `content/docs` |
| `lib/source.ts` | Unified page tree (MDX + OpenAPI) with `loader()` |
| `lib/openapi.ts` | Loads & filters upstream OpenAPI YAML |
| `lib/layout.shared.tsx` | Shared nav config (logo, GitHub URL) for home + docs layouts |
| `lib/public-assets.ts` | `publicAsset()` — basePath-aware asset URLs |
| `scripts/generate-docs.ts` | CLI to regenerate API doc files from OpenAPI |
| `next-sitemap.config.js` | Post-build sitemap/robots generation for GitHub Pages |
| `app/global.css` | Tailwind v4 + fumadocs theme + brand CSS variables (`--brand: #ff7a3c`) |
| `components/home/HomePageClient.tsx` | Full home page client component (~900 lines) |
| `components/home/HeroSection.tsx` | Client component: decorative reel + server `HeroContent` |
| `components/home/HeroContent.tsx` | Server component: hero semantic HTML (SSR-first for SEO) |

---

## Styling Conventions

- **Tailwind CSS v4** (CSS-first config, no `tailwind.config.js`)
- **CSS Modules** for complex component styles (e.g., `HeroSection.module.css`, `HomePageClient.module.css`)
- Brand colors via CSS variables: `--brand`, `--brand-hover`, `--brand-light`
- Display font class: `home-display` (uses Sora)
- Use `--fd-layout-width` (1600px) for max content width via `max-w-(--fd-layout-width)`

---

## SEO Patterns

- **Home page metadata** is defined in `app/(home)/page.tsx` (title, description, keywords, Open Graph, Twitter Card, canonical URL). Root layout provides `metadataBase` and title template.
- **Decorative/animated elements** must carry `aria-hidden="true"` and use empty `alt=""` for decorative images.
- **HeroReel** uses `requestIdleCallback` lazy-mount so its ~120 DOM nodes don't appear in SSR HTML.
- **JSON-LD** (`SoftwareApplication` schema) is injected in `page.tsx` via `<script type="application/ld+json">`.

---

## When Upstream OpenAPI Changes

1. Modify `../../pkg/openapi/native/openapi.yaml` (in the Go project)
2. Run `pnpm generate` — regenerates `content/docs/(api)/` pages
3. Run `pnpm build` to verify