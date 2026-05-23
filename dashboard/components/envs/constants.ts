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

// Constants shared between the Envs page and the components/links that need
// to deep-link to an env's detail sheet (e.g. the Pool list's "Env" column).
//
// Lives in its own file so the Next.js route's page.tsx file only exports a
// default React component, matching the route-file constraints.

/** Query-string key on /clusters/{id}/envs that auto-opens the detail sheet. */
export const ENV_DETAIL_PARAM = "env"
