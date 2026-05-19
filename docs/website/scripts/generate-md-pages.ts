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

/**
 * Copies MDX source files to out/docs/<slug>.md after static export.
 * Lets AI agents fetch raw Markdown by appending .md to any docs URL.
 */
import { readFileSync, writeFileSync, mkdirSync, readdirSync, statSync } from 'fs';
import { join, dirname } from 'path';

const outDir = join(process.cwd(), 'out');
const contentDir = join(process.cwd(), 'content/docs');

function collectMdxFiles(dir: string, relBase = ''): Array<{ absPath: string; slugs: string[] }> {
  const result: Array<{ absPath: string; slugs: string[] }> = [];
  for (const entry of readdirSync(dir)) {
    const abs = join(dir, entry);
    const rel = relBase ? `${relBase}/${entry}` : entry;
    if (statSync(abs).isDirectory()) {
      result.push(...collectMdxFiles(abs, rel));
    } else if (/\.(mdx|md)$/.test(entry)) {
      const name = entry.replace(/\.(mdx|md)$/, '');
      const parts = relBase.split('/').filter((p) => p && !p.startsWith('('));
      if (name !== 'index') parts.push(name);
      result.push({ absPath: abs, slugs: parts });
    }
  }
  return result;
}

function main() {
  const files = collectMdxFiles(contentDir);
  let count = 0;

  for (const { absPath, slugs } of files) {
    const urlPath = slugs.length === 0 ? '/docs' : `/docs/${slugs.join('/')}`;
    const dest = join(outDir, `${urlPath}.md`);
    mkdirSync(dirname(dest), { recursive: true });
    writeFileSync(dest, readFileSync(absPath));
    console.log(`  ${urlPath}.md`);
    count++;
  }

  console.log(`\nGenerated ${count} .md files`);
}

main();
