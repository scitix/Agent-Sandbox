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
 * The examples, as pages.
 *
 * `config/samples/*.yaml` is the source of truth. These are manifests someone
 * applies — to a cluster, with kubectl — and a page that paraphrased them would
 * be a second copy of a manifest, drifting one edit at a time. So the page
 * *is* the file: its header comment becomes the introduction, the manifest sits
 * in a fence exactly as it ships, and the page links to the same file on
 * GitHub's develop branch.
 *
 * Written to gitignored paths for the same reason, and generated before
 * `next build` and `next dev` because fumadocs reads the content directory when
 * it compiles.
 */

import { mkdirSync, readFileSync, writeFileSync } from 'fs';
import { join } from 'path';

/** `docs/website` → the repository root that holds `config/samples`. */
const ossRoot = join(process.cwd(), '..', '..');
const samplesDir = join(ossRoot, 'config', 'samples');
const contentDir = join(process.cwd(), 'content', 'docs', 'examples');
const REPO = 'https://github.com/scitix/Agent-Sandbox/blob/develop/config/samples';

interface Example {
  /** File in config/samples. */
  file: string;
  /** Page slug, and the order the sidebar shows. */
  slug: string;
  title: string;
  summary: string;
}

/**
 * One entry per published sample. The prose lives in the sample's own header
 * comment — that is the copy a reader of the repository sees too — so an entry
 * here is only what the site needs on top: what to call the page, and the one
 * line a list of links shows.
 */
const GROUPS: Array<{ dir: string; title: string; examples: Example[] }> = [
  {
    dir: 'templates',
    title: 'Templates',
    examples: [
      {
        file: 'e2b-basic_sandboxtemplate.yaml',
        slug: 'e2b',
        title: 'E2B Basic',
        summary: 'The base template: envd in a Pod, any image on claim.',
      },
      {
        file: 'e2b-docker_sandboxtemplate.yaml',
        slug: 'e2b-docker',
        title: 'E2B Docker',
        summary: 'Docker inside the sandbox — dockerd and the compose plugin, so a workload can build and run containers.',
      },
      {
        file: 'e2b-kata_sandboxtemplate.yaml',
        slug: 'e2b-kata',
        title: 'E2B Kata',
        summary: 'The same sandbox in its own kernel — a microVM runtime class, for code you do not trust.',
      },
    ],
  },
  {
    dir: 'envs',
    title: 'Environments',
    examples: [
      {
        file: 'e2b-basic_sandboxenv.yaml',
        slug: 'e2b',
        title: 'E2B Basic warm pool',
        summary: 'An environment on the E2B template, with two Pods kept warm.',
      },
      {
        file: 'e2b-docker_sandboxenv.yaml',
        slug: 'e2b-docker',
        title: 'E2B Docker warm pool',
        summary: 'The same shape on the Docker-in-sandbox template.',
      },
      {
        file: 'e2b-kata_sandboxenv.yaml',
        slug: 'e2b-kata',
        title: 'E2B Kata warm pool',
        summary: 'The same shape on the microVM-isolated template.',
      },
    ],
  },
];

/**
 * The leading comment block, as prose.
 *
 * Every line's own indentation goes too: the comments are wrapped and indented
 * for YAML, and Markdown would read a two-space indent as structure it is not.
 */
function intro(manifest: string): string[] {
  const lines: string[] = [];
  for (const line of manifest.split('\n')) {
    if (!line.startsWith('#')) break;
    lines.push(line.replace(/^#\s*/, ''));
  }
  // The first line names the kind of object, which the page's own title and
  // type already say.
  if (/^(SandboxTemplate|SandboxEnv):/.test(lines[0] ?? '')) lines.shift();
  return lines;
}

function page(example: Example): string {
  const source = readFileSync(join(samplesDir, example.file), 'utf8').trimEnd();
  // The header comment is the introduction above, so the fence starts at the
  // manifest itself — printing it twice would read as emphasis it is not.
  const manifest = source.replace(/^(?:#.*\n)+/, '').replace(/^\n+/, '');
  return [
    '---',
    `title: ${example.title}`,
    `description: ${JSON.stringify(example.summary)}`,
    '---',
    '',
    `> Source: [\`config/samples/${example.file}\`](${REPO}/${example.file}) on the`,
    '> `develop` branch — the file this page is rendered from, and the one to',
    '> apply.',
    '',
    ...intro(source),
    '',
    '```yaml',
    manifest,
    '```',
    '',
    '```bash',
    `kubectl apply -f config/samples/${example.file}`,
    '```',
    '',
  ].join('\n');
}

function main() {
  let count = 0;
  for (const group of GROUPS) {
    const dir = join(contentDir, group.dir);
    mkdirSync(dir, { recursive: true });
    for (const example of group.examples) {
      writeFileSync(join(dir, `${example.slug}.mdx`), page(example));
      count++;
    }
    writeFileSync(
      join(dir, 'meta.json'),
      `${JSON.stringify(
        { title: group.title, pages: ['index', ...group.examples.map((e) => e.slug)] },
        null,
        2,
      )}\n`,
    );
  }
  console.log(`  examples: ${count} page(s) from config/samples`);
}

main();
