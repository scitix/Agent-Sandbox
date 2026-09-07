// Framework-free table serialization (Markdown). The pure core of
// lib/table-export.ts: turns plain rows + columns into a GitHub-flavored
// Markdown table. The app's table-export.ts keeps the browser bits (download,
// live-table column extraction) and delegates the matrix/markdown build here.

// A normalized export column: the row-value key plus the resolved header label.
export interface ExportColumn {
  id: string
  header: string
}

// Stringify an arbitrary cell value (arrays joined, objects JSON-encoded).
export function stringifyCell(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') {
    if (Array.isArray(value)) return (value as unknown[]).join('; ')
    return JSON.stringify(value)
  }
  return String(value)
}

// Read the value matrix for the given rows × columns. `valueGetter` reads one
// cell value from a row (defaults to plain property access by column id); the
// app passes a TanStack `row.getValue(id)` getter to reuse live-table values.
export function buildMatrix<T>(
  rows: T[],
  cols: ExportColumn[],
  valueGetter: (row: T, colId: string) => unknown = (row, id) =>
    (row as Record<string, unknown>)[id]
): { headers: string[]; body: string[][] } {
  return {
    headers: cols.map(c => c.header),
    body: rows.map(row =>
      cols.map(c => {
        try {
          return stringifyCell(valueGetter(row, c.id))
        } catch {
          return ''
        }
      })
    ),
  }
}

// A pipe-table cell can't contain a raw pipe or newline.
function mdCell(value: string): string {
  return value.replace(/\|/g, '\\|').replace(/\r?\n/g, ' ')
}

// Render a GitHub-flavored Markdown table from headers + a row matrix.
export function toMarkdownTable(headers: string[], body: string[][]): string {
  if (headers.length === 0) return ''
  const head = `| ${headers.map(mdCell).join(' | ')} |`
  const sep = `| ${headers.map(() => '---').join(' | ')} |`
  const rows = body.map(r => `| ${r.map(mdCell).join(' | ')} |`)
  return [head, sep, ...rows].join('\n')
}

// A titled Markdown block: an h2 heading followed by the table.
export function toMarkdownSection(
  title: string,
  headers: string[],
  body: string[][]
): string {
  return `## ${title}\n\n${toMarkdownTable(headers, body)}`
}

// ── Structured table snapshot ────────────────────────────────────────────
//
// A machine-readable picture of what a table currently shows: its title, the
// active filters (with the dimension's possible values when it's an enum), and
// the filtered rows. This is what the dashboard hands the assistant (Ask AI
// attachment) and downloads on export — the structured counterpart to the old
// Markdown dump, aligned with the assistant's own `ListResult` shape so the UI
// and the agent describe a table the same way. Keys are stable English; the
// human-facing labels (columnTitle / available[].label) carry the localized
// text the caller resolved.

/** One active filter dimension in a snapshot. */
export interface SnapshotFilter {
  /** Column id the filter is bound to. */
  column: string
  /** Localized column title. */
  columnTitle: string
  variant: 'enum' | 'text' | 'range'
  /** Currently-applied value(s); a range is [min, max] with '' for open ends. */
  applied: string[]
  /** All selectable values — present only for enum dimensions. */
  available?: { value: string; label: string }[]
}

/** A structured snapshot of a table's current title, filters and rows. */
export interface TableSnapshot {
  title: string
  /** Source URL, recorded when the table is filtered. */
  url?: string
  /** Global free-text search term, when set. */
  search?: string
  filters: SnapshotFilter[]
  /** Row count before filtering. */
  total: number
  /** Row count after the active filters. */
  matched: number
  columns: ExportColumn[]
  /** Value matrix aligned with `columns`, one inner array per matched row. */
  rows: string[][]
}
