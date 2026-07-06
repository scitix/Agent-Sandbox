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

// i18n-processed-v1.1.0 (no translatable strings)
import { ReactNode } from "react"

export interface DataTableFacetedFilterOption {
  label: ReactNode
  value: string
  isDestructive?: boolean
  count?: number
}

interface DataTableFacetedFilterBaseProps {
  columnKey: string
  title?: string
}

export interface DataTableFacetedFilterDefaultProps extends DataTableFacetedFilterBaseProps {
  variant?: "default"
  options?: DataTableFacetedFilterOption[]
  emptyOption?: {
    label: ReactNode
  }
  renderer?: (value: string) => ReactNode
  defaultValues?: string[]
}

export interface DataTableFacetedFilterNumberRangeProps extends DataTableFacetedFilterBaseProps {
  variant: "number_range"
  unit?: string
  defaultValues?: [number?, number?]
  placeholder?: {
    min?: string
    max?: string
  }
}

export interface DataTableFacetedFilterTextProps extends DataTableFacetedFilterBaseProps {
  variant: "text"
  placeholder?: string
  defaultValues?: string[]
}

export type DataTableFacetedFilterProps =
  | DataTableFacetedFilterDefaultProps
  | DataTableFacetedFilterNumberRangeProps
  | DataTableFacetedFilterTextProps
