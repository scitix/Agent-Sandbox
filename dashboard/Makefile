.PHONY: code-formatter env

ENV_FILE ?= .env.local

code-formatter: ## Run all code formatting tools.
	pnpm exec prettier --write . --log-level warn

env: ## Scaffold $(ENV_FILE) with the variables documented in README.md (git-ignored).
	@if [ -e "$(ENV_FILE)" ]; then \
		echo "$(ENV_FILE) already exists — refusing to overwrite. Remove it first or set ENV_FILE=..."; \
		exit 1; \
	fi
	@printf '%s\n' \
		'# Dashboard environment — see README.md "Environment Variables" for full reference.' \
		'# This file is loaded by Next.js during `pnpm dev` and is git-ignored (.env*.local).' \
		'' \
		'# ── Required ──────────────────────────────────────────────────────────────' \
		'# HS256 secret for session JWTs. Generate with: openssl rand -hex 32' \
		'JWT_SECRET=' \
		'' \
		'# ── BFF ↔ wsproxy sidecar ────────────────────────────────────────────────' \
		'WSPROXY_INTERNAL_URL=http://localhost:9004' \
		'AGENTBOX_MANAGER_TOKEN=' \
		'' \
		'# ── Prometheus (optional; unset ⇒ metrics pages show "not configured") ──' \
		'# PROMETHEUS_URL=http://prometheus.monitoring.svc:9090' \
		'# PROMETHEUS_TOKEN=' \
		'' \
		'# ── Log download (optional; all three required to enable) ───────────────' \
		'# LOG_DOWNLOAD_URL=' \
		'# LOG_APP_ID=' \
		'# LOG_TOKEN=' \
		'' \
		'# ── OIDC / Dex SSO (optional; DEX_ISSUER_URL toggles the feature) ──────' \
		'# DEX_ISSUER_URL=' \
		'# DEX_REDIRECT_URI=' \
		'# DEX_CLIENT_ID=' \
		'# DEX_CLIENT_SECRET=' \
		'# DEX_USERINFO_URI=' \
		'# DEX_OIDC_ADMINS=' \
		'' \
		'# ── File paths (optional; defaults target in-cluster deployments) ──────' \
		'# CLUSTERS_CONFIG_PATH=/etc/agentbox/clusters.yaml' \
		'# IMAGES_CATALOG_PATH=/etc/agentbox/images-catalog.json' \
		'# AUDIT_LOG_PATH=' \
		'' \
		'# ── Next.js base path / public (build-time) ────────────────────────────' \
		'# NEXT_BASE_PATH=' \
		'# NEXT_PUBLIC_BASE_PATH=' \
		'# NEXT_PUBLIC_APP_VERSION=0.0.0' \
		> "$(ENV_FILE)"
	@echo "Wrote $(ENV_FILE). Fill in JWT_SECRET at minimum before running \`pnpm dev\`."
