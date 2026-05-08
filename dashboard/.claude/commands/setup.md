# /setup — Dashboard Project Setup

Run this command to verify or reinitialize the dashboard development environment.

## What this command does

1. **Confirm dependencies are installed** — run `pnpm install` if `node_modules` is missing or stale
2. **Run code formatter** — `make code-formatter` (prettier + eslint --fix)
3. **Run type check** — `pnpm exec tsc --noEmit`
4. **Run tests** — `pnpm test`
5. **Attempt a production build** — `pnpm build`

## Steps

```bash
# 1. Install dependencies
pnpm install

# 2. Format & lint
make code-formatter

# 3. Type check
pnpm exec tsc --noEmit

# 4. Unit tests
pnpm test

# 5. Production build
pnpm build
```

Report the result of each step. If any step fails, show the error and stop — do not proceed to the next step.

## Project notes

- **Package manager**: pnpm
- **Framework**: Next.js 16 (App Router, Turbopack)
- **Formatter**: Prettier (`pnpm exec prettier --write . --log-level warn`)
- **Linter**: ESLint flat config (`eslint.config.mjs`), run via `pnpm exec eslint . --fix`
- **Formatter + linter together**: `make code-formatter`
- **Tests**: Vitest (`pnpm test`)
- **.prettierrc**: located at `dashboard/.prettierrc`
- **eslint config**: `dashboard/eslint.config.mjs`
