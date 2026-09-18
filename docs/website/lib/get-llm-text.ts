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

import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { source } from '@/lib/source';
import { stripFrontmatter, toMarkdown } from '@/lib/mdx-to-markdown';

export async function getLLMText(page: (typeof source)['$inferPage']) {
  // The source file rather than fumadocs' processed markdown, so the llms text
  // and the `.md` twin of the same page are the same document: a diagram stays
  // a `mermaid` fence instead of becoming a `<Mermaid chart="…"/>` element, and
  // a tab becomes its heading instead of JSX.
  const file = join(process.cwd(), 'content/docs', page.path ?? '');
  if (!page.path || !existsSync(file)) return null;

  const body = toMarkdown(stripFrontmatter(readFileSync(file, 'utf8')), page.path);
  return `# ${page.data.title} (${page.url})

${body}`;
}
