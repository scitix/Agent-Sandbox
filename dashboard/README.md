# Agent Sandbox Dashboard

Next.js frontend for Agent Sandbox — a Kubernetes-native sandbox management platform.

## Tech Stack

| Layer         | Library                           |
| ------------- | --------------------------------- |
| Framework     | Next.js 15 (App Router)           |
| UI Components | shadcn/ui (Base UI primitives)    |
| Data Fetching | TanStack Query v5 + openapi-fetch |
| Forms         | react-hook-form + Zod             |
| State         | Jotai                             |
| Styling       | Tailwind CSS v4                   |
| Charts        | Recharts + Prometheus             |
| Testing       | Vitest                            |

## Getting Started

```bash
pnpm install
make env          # Scaffold .env.local with required keys (git-ignored)
# Edit .env.local and fill in JWT_SECRET at minimum
pnpm dev          # http://localhost:3000
```

## Environment Variables

All variables are read via `process.env.*`. `.env.local` is git-ignored (see `.gitignore`) and
loaded automatically by Next.js for local development. In production they must be injected
by the runtime (K8s Deployment env / ConfigMap / Secret).

### Required

| Variable     | Purpose                                                                                                            |
| ------------ | ------------------------------------------------------------------------------------------------------------------ |
| `JWT_SECRET` | HS256 secret used to sign session JWTs (`lib/auth.ts`). **Must be set** — the server throws on startup without it. |

### BFF ↔ Sidecar (required in-cluster, optional locally)

| Variable                 | Default                 | Purpose                                                                                                      |
| ------------------------ | ----------------------- | ------------------------------------------------------------------------------------------------------------ |
| `WSPROXY_INTERNAL_URL`   | `http://localhost:9004` | Base URL of the `wsproxy` sidecar's internal control-plane API (global-template / global-apikey BFF routes). |
| `AGENTBOX_MANAGER_TOKEN` | `""`                    | Bearer token forwarded by BFF routes to the manager cluster.                                                 |

### Observability — Prometheus

| Variable           | Required                  | Purpose                                                                                                                      |
| ------------------ | ------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `PROMETHEUS_URL`   | If metrics pages are used | Prometheus query endpoint (`/api/v1/query_range`). When unset, `/api/prometheus/*` routes short-circuit to "not configured". |
| `PROMETHEUS_TOKEN` | Optional                  | Bearer token for Prometheus.                                                                                                 |

### Log download (optional)

All three must be set together to enable the in-app log download feature; otherwise
`/api/sandbox-logs/config` reports the feature as disabled.

| Variable           | Purpose                                 |
| ------------------ | --------------------------------------- |
| `LOG_DOWNLOAD_URL` | Upstream log service URL.               |
| `LOG_APP_ID`       | App ID registered with the log service. |
| `LOG_TOKEN`        | Bearer token for the log service.       |

### OIDC / Dex SSO (optional)

Setting `DEX_ISSUER_URL` enables OIDC login. When enabled, `DEX_CLIENT_ID`, `DEX_CLIENT_SECRET`
and `DEX_REDIRECT_URI` must also be set.

| Variable            | Purpose                                                                  |
| ------------------- | ------------------------------------------------------------------------ |
| `DEX_ISSUER_URL`    | Dex / OIDC issuer URL. Presence toggles OIDC login.                      |
| `DEX_REDIRECT_URI`  | OAuth callback URL registered with the IdP.                              |
| `DEX_CLIENT_ID`     | OIDC client ID.                                                          |
| `DEX_CLIENT_SECRET` | OIDC client secret.                                                      |
| `DEX_USERINFO_URI`  | Optional userinfo override used during callback.                         |
| `DEX_OIDC_ADMINS`   | Admin mapping, e.g. `org1:alice,bob;org2:carol`. Empty ⇒ no OIDC admins. |

### File paths (optional, container-friendly defaults)

| Variable               | Default                             | Purpose                                                                      |
| ---------------------- | ----------------------------------- | ---------------------------------------------------------------------------- |
| `CLUSTERS_CONFIG_PATH` | `/etc/agentbox/clusters.yaml`       | Location of the clusters registry consumed by the BFF `getClusters()` route. |
| `IMAGES_CATALOG_PATH`  | `/etc/agentbox/images-catalog.json` | Curated image catalog surfaced in create dialogs.                            |
| `AUDIT_LOG_PATH`       | `""` (disabled)                     | If set, audit events are appended to this file.                              |

### Next.js build / public (exposed to browser)

| Variable                  | Purpose                                                                                                                |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `NEXT_BASE_PATH`          | Build-time base path (e.g. `/dashboard`) wired into `next.config.mjs`.                                                 |
| `NEXT_PUBLIC_BASE_PATH`   | Runtime mirror of the base path used by client fetches, WebSocket URLs, and redirects. Keep equal to `NEXT_BASE_PATH`. |
| `NEXT_PUBLIC_APP_VERSION` | Displayed in the changelog trigger; injected at build time (default `"0.0.0"`).                                        |

## Key Commands

```bash
pnpm build                        # Production build
pnpm exec tsc --noEmit            # Type check
pnpm test                         # Run tests (Vitest)

# API types — run after editing pkg/openapi/native/openapi.yaml
pnpm run gen:types                # Regenerate lib/api/schema.d.ts

# i18n
pnpm i18n:gen-types               # Generate messages/_schema.ts from en.json
pnpm i18n:validate                # Validate all locale files against en.json
pnpm i18n:extract                 # Scan code for hardcoded strings
```

## Project Structure

```
app/                    Next.js App Router pages
components/
  ui/                   Auto-generated shadcn components (do not edit)
  <domain>/             Business components (sandboxes, pools, templates, …)
lib/
  api/
    schema.d.ts         Auto-generated OpenAPI types
    client.ts           OpenAPI fetch client + BFF functions + type aliases
  queries/              TanStack Query queryOptions + useMutation per resource
  i18n/                 Internationalization context + hooks
messages/
  en.json               English translations (Single Source of Truth)
  zh-Hans.json          Simplified Chinese translations
  _schema.ts            Auto-generated TranslationKey type
scripts/
  i18n-*.ts             i18n tooling (extract / validate / gen-types)
docs/
  form-example.tsx      Canonical form pattern reference
```

## Architecture

### API Flow (OpenAPI-first)

```
pkg/openapi/native/openapi.yaml
  → lib/api/schema.d.ts  (auto-generated, never edit manually)
  → lib/api/client.ts    (typed clients + BFF routes)
  → lib/queries/         (queryOptions + mutations per resource)
  → React components
```

Two client types:

- **`currentApiClient()`** — `openapi-react-query` client, per-cluster, for all cluster-scoped resources.
- **BFF raw fetch** (`getClusters`, `global-apikey`, `global-template`) — plain `fetch()` against Next.js API routes, used for cross-cluster / control-plane operations.

### Data Rules

- Never use raw `fetch()` or `useEffect + useState` in components — all server data flows through TanStack Query.
- List endpoints use `select: (data) => data.items ?? []` so components receive a plain array.
- Dialogs that fetch data follow the **Shell + Inner Form** pattern — the inner form is mounted only when the dialog is `open`, so queries only fire when needed.
- See [`components/sandboxes/create-dialog.tsx`](./components/sandboxes/create-dialog.tsx) for the canonical form + Combobox pattern.

### i18n

URL-first bilingual support (English default, `/zh/` prefix for Simplified Chinese):

- `messages/en.json` is the **Single Source of Truth** — add new keys here first, then sync other locales.
- Run `pnpm i18n:gen-types` after editing `en.json` to keep TypeScript types in sync.
- `pnpm i18n:validate` runs in CI to catch missing or mismatched keys.

## Contributing

Before submitting changes:

1. `pnpm exec tsc --noEmit` — zero type errors
2. `pnpm test` — all tests pass
3. `pnpm i18n:validate` — all locale files in sync
