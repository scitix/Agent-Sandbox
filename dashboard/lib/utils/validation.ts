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

import { z } from "zod"

/**
 * RFC 1123 DNS label, letter-start variant.
 * - Must start with a lowercase letter [a-z]
 * - Middle characters: lowercase letters, digits, or hyphens [a-z0-9-]
 * - Must end with a lowercase letter or digit [a-z0-9]
 * - Single-character names (a single letter) are valid
 * - Maximum 63 characters
 */
export const k8sNameSchema = z
  .string()
  .min(1, "Name is required")
  .max(63, "Must be at most 63 characters")
  .regex(
    /^[a-z]([a-z0-9-]*[a-z0-9])?$/,
    "Must start with a lowercase letter, end with alphanumeric, only [a-z0-9-] allowed",
  )
