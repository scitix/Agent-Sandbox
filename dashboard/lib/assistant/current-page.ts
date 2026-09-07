/**
 * Where the user is, for the `<page/>` marker the assistant receives.
 *
 * The reference implementation derives this from a registry shared with its
 * navigation tool. We have no such tool, so this reads the route directly —
 * and reads only what the marker is allowed to mean: which page, and which
 * cluster if the route is cluster-scoped.
 *
 * The prompt is explicit that this is where the user IS, not where to look, so
 * a wrong guess here costs a slightly-off demonstrative rather than a search in
 * the wrong place.
 */
export interface CurrentPage {
  key: string
  cluster?: string
}

const LOCALE = /^\/(en|zh-Hans|zh-Hant)(?=\/|$)/

export function stripLocale(pathname: string, base = ""): string {
  let p = pathname
  if (base && p.startsWith(base)) p = p.slice(base.length)
  return p.replace(LOCALE, "") || "/"
}

/** The page key, or undefined at the root. */
export function matchCurrentPage(
  pathname: string,
  base = ""
): CurrentPage | undefined {
  const p = stripLocale(pathname, base).replace(/\/+$/, "")
  if (!p || p === "/") return undefined
  const parts = p.split("/").filter(Boolean)
  // /clusters/<id>/<page>[/...]
  if (parts[0] === "clusters" && parts[1]) {
    const key = parts[2] ?? "clusters"
    return { key, cluster: parts[1] }
  }
  return { key: parts[0] }
}

/** The cluster the route names, when it names one. */
export function clusterFromPath(
  pathname: string,
  base = ""
): string | undefined {
  return matchCurrentPage(pathname, base)?.cluster
}
