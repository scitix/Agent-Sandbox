// Hierarchical fold regions for block-indented text (YAML / JSON).
//
// Everything the dashboard renders in a code block is machine-serialised with a
// stable indent — `js-yaml` dumps, `JSON.stringify(x, null, 2)`, and the API's
// server-rendered YAML — so the indentation *is* the hierarchy and a single
// linear walk recovers it. No grammar, no parser, and it degrades sanely on any
// other block-indented payload (a log with indented stack frames still folds).
//
// A region is owned by the line that introduces it and hides only the lines
// *below* it, which is what editors do: the closing `}` of a JSON object is
// dedented back to the owner's level, so it stays visible and the collapsed
// block still reads as `"spec": { … }`.
//
// The one place raw indent lies is a YAML block sequence, because the `- `
// entries of a mapping key are conventionally written at the key's own indent —
// which is what Go's yaml marshaller emits, so it is what the API's node and pod
// YAML look like:
//
//   conditions:            <- indent 2
//   - type: Ready          <- indent 2 as well, yet a child of `conditions`
//     status: "True"       <- indent 4
//
// Comparing visual indent alone would leave `conditions` unfoldable. So depth is
// ranked as `indent * 2 + (entry ? 1 : 0)`: a `- ` entry sits one step below a
// mapping key at the same indent, one entry closes the previous one, and a
// dedented key closes them all. Sequences written with the indented style
// (`js-yaml`'s default) and JSON — which never starts a line with `- ` — rank
// exactly as plain indent would.

export interface FoldRegion {
  /** 0-based index of the line that carries the fold toggle. */
  start: number
  /** 0-based index of the last line hidden while the region is collapsed. */
  end: number
  /** Nesting depth; 1 for a region at the outermost indent. */
  level: number
}

/**
 * Depth rank of a line, or `null` when the line carries no content. Blank lines
 * have no depth of their own — they belong to whatever encloses them — so they
 * never open or close a region.
 */
function depthRank(line: string): number | null {
  let indent = 0
  let i = 0
  for (; i < line.length; i++) {
    const ch = line[i]
    if (ch === ' ') indent += 1
    else if (ch === '\t') indent += 2
    else break
  }
  if (i === line.length) return null
  // `- x` / a bare `-` opens a sequence entry; `---` is a document separator.
  const rest = line.slice(i)
  const isEntry =
    rest === '-' || rest.startsWith('- ') || rest.startsWith('-\t')
  return indent * 2 + (isEntry ? 1 : 0)
}

/**
 * Fold regions for `lines`, ordered by start line. Linear in the number of
 * lines: a stack holds the regions still open at the cursor, and a line whose
 * depth is not below the top of the stack closes it.
 */
export function computeFoldRegions(lines: readonly string[]): FoldRegion[] {
  const regions: FoldRegion[] = []
  // Candidate owners, strictly increasing depth from bottom to top.
  const open: { start: number; rank: number }[] = []
  // Last line with content — the end of any region closing at the next line.
  let lastContent = -1

  // Close every open region the line at `rank` has dedented out of; a null rank
  // closes all of them (end of input).
  const closeDownTo = (rank: number | null) => {
    for (let owner = open.at(-1); owner != null; owner = open.at(-1)) {
      if (rank !== null && owner.rank < rank) return
      open.pop()
      // A region needs at least one line under its owner to be worth folding.
      if (lastContent > owner.start) {
        regions.push({
          start: owner.start,
          end: lastContent,
          level: open.length + 1,
        })
      }
    }
  }

  for (let i = 0; i < lines.length; i++) {
    const rank = depthRank(lines[i])
    if (rank === null) continue
    closeDownTo(rank)
    open.push({ start: i, rank })
    lastContent = i
  }
  closeDownTo(null)

  regions.sort((a, b) => a.start - b.start)
  return regions
}

/** Deepest nesting level present, or 0 when nothing folds. */
export function maxFoldLevel(regions: readonly FoldRegion[]): number {
  let max = 0
  for (const r of regions) if (r.level > max) max = r.level
  return max
}

/**
 * Owner lines to collapse so that only `level - 1` levels stay expanded.
 * `foldStartsFromLevel(regions, 1)` collapses everything; level 2 keeps the
 * outermost keys open and folds their children; and so on.
 */
export function foldStartsFromLevel(
  regions: readonly FoldRegion[],
  level: number
): number[] {
  return regions.filter(r => r.level >= level).map(r => r.start)
}

/**
 * Per-line "is hidden" flags for a set of collapsed owner lines. A collapsed
 * region hides its whole body, including the owners of any nested regions —
 * their own collapsed state is kept, so re-expanding the parent restores it.
 */
export function computeHiddenLines(
  lineCount: number,
  regions: readonly FoldRegion[],
  collapsed: ReadonlySet<number>
): boolean[] {
  const hidden = new Array<boolean>(lineCount).fill(false)
  if (collapsed.size === 0) return hidden
  for (const r of regions) {
    if (!collapsed.has(r.start)) continue
    const end = Math.min(r.end, lineCount - 1)
    for (let i = r.start + 1; i <= end; i++) hidden[i] = true
  }
  return hidden
}
