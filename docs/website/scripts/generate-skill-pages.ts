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
 * The nine skills, as pages.
 *
 * `plugin/skills/*&#47;SKILL.md` is the source of truth: the installer fetches
 * those files into `~/.agents/skills`, and an agent reads them there. This
 * script renders the same files into `content/docs/skills/` so they
 * can be read — and reviewed — in a browser, with a `.md` twin for an agent.
 *
 * Generated rather than copied by hand, and written to a gitignored path: a
 * checked-in copy is a second source of truth, and the drift would be silent in
 * exactly the direction that matters (the page a human reviews saying something
 * the file the agent reads does not).
 *
 * Runs before `next build` and before `next dev`, because fumadocs reads the
 * content directory when it compiles.
 */

import { mkdirSync, readdirSync, readFileSync, writeFileSync } from 'fs';
import { join } from 'path';
import { load } from 'js-yaml';

/** `docs/website` → the repository root that holds `plugin/skills`. */
const ossRoot = join(process.cwd(), '..', '..');
const skillsDir = join(ossRoot, 'plugin', 'skills');
const outDir = join(process.cwd(), 'content', 'docs', 'skills');

interface Skill {
  /** Directory name — what `~/.agents/skills` will hold. */
  name: string;
  /** One line for the frontmatter, from the skill's own frontmatter. */
  description: string;
  /** The skill's opening heading, demoted to a section. */
  heading?: string;
  body: string;
}

function readSkill(name: string): Skill {
  const src = readFileSync(join(skillsDir, name, 'SKILL.md'), 'utf8');
  const fm = src.match(/^---\n([\s\S]*?)\n---\n?/);
  if (!fm) throw new Error(`plugin/skills/${name}/SKILL.md has no frontmatter`);
  const meta = load(fm[1]) as { name?: string; description?: string };
  if (!meta.description) {
    // Without it the page has no summary, and llms.txt has a row with no text.
    throw new Error(`plugin/skills/${name}/SKILL.md has no description`);
  }

  const rest = src.slice(fm[0].length);
  const heading = rest.match(/^\s*#\s+(.+)$/m)?.[1]?.trim();
  // The page's <h1> is the layout's, from the frontmatter: the skill's own
  // opening heading becomes the first section so nothing is lost and nothing
  // is printed twice.
  const body = rest.replace(/^\s*#\s+.+\n+/, '').trim();
  return { name, description: meta.description, heading, body };
}

function page(skill: Skill): string {
  const source = `https://github.com/scitix/Agent-Sandbox/blob/develop/plugin/skills/${skill.name}/SKILL.md`;
  return [
    '---',
    `title: ${skill.name}`,
    // Quoted, because these descriptions contain colons and em dashes.
    `description: ${JSON.stringify(skill.description)}`,
    '---',
    '',
    '<Callout title="Generated from">',
    `[\`plugin/skills/${skill.name}/SKILL.md\`](${source}) — the same file the installer`,
    'writes to `~/.agents/skills`. Edit it there; this page follows.',
    '</Callout>',
    '',
    skill.heading ? `## ${skill.heading}` : '',
    '',
    skill.body,
    '',
  ]
    .filter((line, i, all) => !(line === '' && all[i - 1] === ''))
    .join('\n');
}

function main() {
  const names = readdirSync(skillsDir, { withFileTypes: true })
    .filter((e) => e.isDirectory() && !e.name.startsWith('.'))
    .map((e) => e.name)
    .sort();

  mkdirSync(outDir, { recursive: true });
  for (const name of names) {
    writeFileSync(join(outDir, `${name}.mdx`), page(readSkill(name)));
  }
  // The sidebar order comes from here, so a new skill appears without anyone
  // remembering to list it.
  writeFileSync(
    join(outDir, 'meta.json'),
    `${JSON.stringify({ title: 'Skills', pages: ['index', ...names] }, null, 2)}\n`,
  );
  console.log(`  skills: ${names.length} page(s) from plugin/skills`);
}

main();
