// The id a stat-card overlay column would use. Declared here rather than
// imported: the component that renders those overlays is not part of this
// build, but a snapshot must still know never to export the column.
declare module "@tanstack/react-table" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    /** The header label, stamped by the table so an export reads like the UI. */
    headerTitle?: string
    /**
     * How this column is filtered, when it is. `options` is present only for
     * the enum variant — the exporter reads it to record what the user COULD
     * have picked alongside what they did, so a snapshot is readable without
     * the table beside it.
     */
    filter?: {
      variant?: "text" | "number_range" | string
      options?: { value: string; label?: string }[]
    }
  }
}

const STAT_CARD_OVERLAY_COL_ID = "__stat_card_overlay__"
import type { TranslationKey } from '@/lib/i18n'

/** Just the lookup this module needs. */
type Translator = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string
import {
  type ExportColumn,
  type TableSnapshot,
  buildMatrix as buildMatrixCore,
} from '@/lib/assistant/serialize'
import {
  type ColumnDef,
  type Row,
  type RowData,
  type Table,
} from '@tanstack/react-table'

declare module '@tanstack/react-table' {
  // Augment the per-column meta so a column can opt out of CSV/Markdown export
  // (e.g. verbose labels/annotations). Defaults to exportable when unset.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    exportable?: boolean
  }
}

// The matrix/markdown primitives are version-agnostic and live in the headless
// core (the opencode tools serialize the same way). Re-exported here so existing
// `lib/table-export` imports are unaffected.
export {
  type ExportColumn,
  stringifyCell,
  toMarkdownTable,
  toMarkdownSection,
} from '@/lib/assistant/serialize'

// Resolve a column's header label from its stamped `meta.headerTitle` (set by
// resolveDataColumns from the column's headerConfig), falling back to a plain
// string header, then the column id.
export function columnHeaderLabel<T>(
  col: ColumnDef<T, unknown>,
  fallbackId?: string,
  t?: Translator
): string {
  const id = (col.id ?? fallbackId) || ''
  const title = col.meta?.headerTitle
  if (title) return title
  if (typeof col.header === 'string' && col.header) return col.header
  return id || (t ? t('table.unknownColumn') : 'Unknown column')
}

// Export columns from a raw ColumnDef array (keeps only data columns not opted
// out via `meta.exportable: false`).
export function columnsFromDefs<T>(
  columns: ColumnDef<T, unknown>[],
  t?: Translator
): ExportColumn[] {
  return columns
    .filter(col => col.id && col.meta?.exportable !== false)
    .map(col => ({
      id: col.id as string,
      header: columnHeaderLabel(col, col.id, t),
    }))
}

// Export columns from a live table: the currently-visible, data-bearing columns
// not opted out via `meta.exportable: false` (so the export reflects the user's
// column toggles and skips select/expand).
export function columnsFromTable<T>(
  table: Table<T>,
  t?: Translator
): ExportColumn[] {
  return table
    .getVisibleLeafColumns()
    .filter(
      col =>
        col.accessorFn !== undefined && col.columnDef.meta?.exportable !== false
    )
    .map(col => ({
      id: col.id,
      header: columnHeaderLabel(
        col.columnDef as ColumnDef<T, unknown>,
        col.id,
        t
      ),
    }))
}

// Read the value matrix for the given live-table rows × columns. Delegates to
// the headless matrix builder with a TanStack `row.getValue` accessor.
export function buildMatrix<T>(
  rows: Row<T>[],
  cols: ExportColumn[]
): { headers: string[]; body: string[][] } {
  return buildMatrixCore(rows, cols, (row, id) => row.getValue(id))
}

// Compact timestamp (YYYYMMDDHHmm) for export filenames, e.g. 202606121101.
export function exportTimestamp(d: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}${p(
    d.getHours()
  )}${p(d.getMinutes())}`
}

// "<cluster>-<last route segment>" derived from the current location, e.g.
// "prod-foo-nodes"; either part is dropped when absent.
export function routeExportBaseName(
  cluster?: string,
  pathname: string = window.location.pathname
): string {
  const segments = pathname.split('/').filter(Boolean)
  const last = segments[segments.length - 1] || 'export'
  return [cluster, last].filter(Boolean).join('-')
}

// Trigger a browser download of a text blob.
export function downloadTextFile(
  content: string,
  filename: string,
  mime: string
) {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.setAttribute('href', url)
  link.setAttribute('download', filename)
  link.style.visibility = 'hidden'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// Trigger a browser download of base64-encoded bytes (binary files, images).
export function downloadBase64File(
  base64: string,
  filename: string,
  mime: string = 'application/octet-stream'
) {
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const blob = new Blob([bytes], { type: mime })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.setAttribute('href', url)
  link.setAttribute('download', filename)
  link.style.visibility = 'hidden'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// Build a structured snapshot of a live table: its title, the active filters
// (with each enum dimension's possible values, read from the column's stamped
// `meta.filter`), and the filtered rows. Handed to the assistant as an Ask AI
// attachment and written out on export.
export function buildTableSnapshot<T>(
  table: Table<T>,
  options: { title: string; url?: string; t?: Translator }
): TableSnapshot {
  const { title, url, t } = options
  const cols = columnsFromTable(table, t)
  const filteredRows = table.getFilteredRowModel().rows
  const { body } = buildMatrix(filteredRows, cols)
  const state = table.getState()

  const filters: TableSnapshot['filters'] = []
  for (const f of state.columnFilters ?? []) {
    // The stat-card overlay is a synthetic hidden filter, not a user filter —
    // never surface it in the snapshot's filter list.
    if (f.id === STAT_CARD_OVERLAY_COL_ID) continue
    const meta = table.getColumn(f.id)?.columnDef.meta
    const spec = meta?.filter
    const variant: 'enum' | 'text' | 'range' =
      spec?.variant === 'number_range'
        ? 'range'
        : spec?.variant === 'text'
          ? 'text'
          : 'enum'
    const applied = Array.isArray(f.value)
      ? f.value.map(v => (v == null ? '' : String(v)))
      : [String(f.value ?? '')]
    const available =
      variant === 'enum' && spec && 'options' in spec && spec.options
        ? spec.options.map(o => ({
            value: o.value,
            label: typeof o.label === 'string' ? o.label : o.value,
          }))
        : undefined
    filters.push({
      column: f.id,
      columnTitle: meta?.headerTitle ?? f.id,
      variant,
      applied,
      available,
    })
  }

  return {
    title,
    url,
    search: state.globalFilter ? String(state.globalFilter) : undefined,
    filters,
    total: table.getCoreRowModel().rows.length,
    matched: filteredRows.length,
    columns: cols,
    rows: body,
  }
}

// Download a table snapshot as a structured JSON document.
export function exportTableSnapshot(snapshot: TableSnapshot, filename: string) {
  downloadTextFile(
    JSON.stringify(snapshot, null, 2),
    `${filename}.json`,
    'application/json;charset=utf-8;'
  )
}
