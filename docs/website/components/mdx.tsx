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

import defaultMdxComponents from 'fumadocs-ui/mdx';
import { File, Files, Folder } from 'fumadocs-ui/components/files';
import { Step, Steps } from 'fumadocs-ui/components/steps';
import { Tab, Tabs } from 'fumadocs-ui/components/tabs';
import { Mermaid } from '@/components/mermaid';

// Re-export a typed getMDXComponents without depending on the external `mdx` package.
//
// Components are registered rather than imported per page so a page stays a
// document: `scripts/generate-md-pages.ts` has to turn every one of these back
// into plain Markdown for the `.md` twin, and a page that imports its own
// components would put an import line in the middle of what an agent fetches.
// Anything registered here needs a case in that transform.
export function getMDXComponents(components?: Record<string, unknown>) {
  return {
    ...defaultMdxComponents,
    Tabs,
    Tab,
    Steps,
    Step,
    Files,
    File,
    Folder,
    Mermaid,
    ...components,
  };
}

export const useMDXComponents = getMDXComponents;
