/**
 * The deploy-time sub-path this dashboard is served under.
 *
 * Baked into the bundle at build time (NEXT_BASE_PATH → next.config's
 * basePath), so it is read from the same env var Next itself uses rather than
 * re-derived. Returns "" at the root, and never a trailing slash — callers that
 * want one use basePathPrefix().
 */
export function basePath(): string {
  const raw = process.env.NEXT_PUBLIC_BASE_PATH ?? ""
  return raw === "/" ? "" : raw.replace(/\/+$/, "")
}

/** basePath() with a trailing slash, for prefixing a relative path. */
export function basePathPrefix(): string {
  const base = basePath()
  return base ? `${base}/` : "/"
}
