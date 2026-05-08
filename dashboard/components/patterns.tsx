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

"use client"

import { cn } from "@/lib/utils"

export function DotPattern({ className }: { className?: string }) {
  return (
    <svg className={cn("absolute inset-0 h-full w-full", className)} aria-hidden="true">
      <defs>
        <pattern id="dot-pattern" x="0" y="0" width="16" height="16" patternUnits="userSpaceOnUse">
          <circle cx="1" cy="1" r="0.8" className="fill-pattern-dot" />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#dot-pattern)" />
    </svg>
  )
}

export function CrossPattern({ className }: { className?: string }) {
  return (
    <svg className={cn("absolute inset-0 h-full w-full", className)} aria-hidden="true">
      <defs>
        <pattern
          id="cross-pattern"
          x="0"
          y="0"
          width="24"
          height="24"
          patternUnits="userSpaceOnUse"
        >
          <line x1="0" y1="12" x2="24" y2="12" className="stroke-pattern-line" strokeWidth="0.5" />
          <line x1="12" y1="0" x2="12" y2="24" className="stroke-pattern-line" strokeWidth="0.5" />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#cross-pattern)" />
    </svg>
  )
}

export function GridPattern({ className }: { className?: string }) {
  return (
    <svg className={cn("absolute inset-0 h-full w-full", className)} aria-hidden="true">
      <defs>
        <pattern id="grid-pattern" x="0" y="0" width="32" height="32" patternUnits="userSpaceOnUse">
          <path
            d="M 32 0 L 0 0 0 32"
            fill="none"
            className="stroke-pattern-line"
            strokeWidth="0.5"
          />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#grid-pattern)" />
    </svg>
  )
}
