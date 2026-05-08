/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { type ColumnDef, type Row } from "@tanstack/react-table"

export function exportToCSV<T>(
  rows: Row<T>[],
  columns: ColumnDef<T, unknown>[],
  filename: string = "export",
) {
  const validColumns = columns.filter((col) => col.id)

  const headers = validColumns.map((col) => {
    if (typeof col.header === "function") {
      try {
        const headerResult = col.header({
          column: {
            id: col.id,
            getCanSort: () => false,
            getIsSorted: () => false,
            toggleSorting: () => { },
            clearSorting: () => { },
          },
        } as never)

        if (typeof headerResult === "object" && headerResult && "props" in headerResult) {
          return (headerResult.props as { title?: string })?.title || col.id || "Unknown"
        }
      } catch {
        // fallback
      }
      return col.id || "Unknown"
    }
    return (col.header as string) || col.id || "Unknown"
  })

  const csvRows = [
    headers,
    ...rows.map((row) =>
      validColumns.map((col) => {
        let value: unknown = ""
        try {
          if (col.id) {
            value = row.getValue(col.id)
          }
        } catch {
          value = ""
        }

        if (value === null || value === undefined) return ""
        if (typeof value === "object") {
          if (Array.isArray(value)) return (value as unknown[]).join("; ")
          return JSON.stringify(value)
        }
        const stringValue = String(value)
        if (stringValue.includes(",") || stringValue.includes('"') || stringValue.includes("\n")) {
          return `"${stringValue.replace(/"/g, '""')}"`
        }
        return stringValue
      }),
    ),
  ]

  const csvContent = csvRows.map((row) => row.join(",")).join("\n")
  const blob = new Blob(["\uFEFF" + csvContent], {
    type: "text/csv;charset=utf-8;",
  })
  const link = document.createElement("a")
  const url = URL.createObjectURL(blob)
  link.setAttribute("href", url)
  link.setAttribute("download", `${filename}.csv`)
  link.style.visibility = "hidden"
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}
