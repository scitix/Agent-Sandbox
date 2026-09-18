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
 * MDX → Markdown, for the two surfaces that are documents rather than pages:
 * the `.md` twin of every docs URL, and `llms-full.txt`.
 *
 * Components are a rendering device — tabs for two ways of doing one thing, a
 * callout for an aside — and an agent fetching the document wants what they
 * say, in the order a person reads it. Every component a page may use has a
 * case here, and a page that uses one it does not know is a build failure
 * rather than a document full of JSX.
 *
 * Both surfaces call this, so the `.md` twin and the llms text cannot disagree
 * about what a page says.
 */

/** `items={['A', 'B']}` / `items={["A"]}` → the labels, in order. */
function tabLabels(attrs: string): string[] {
  const list = attrs.match(/items=\{\[([^\]]*)\]\}/)?.[1] ?? '';
  return [...list.matchAll(/['"]([^'"]+)['"]/g)].map((m) => m[1]);
}

function attr(attrs: string, name: string): string | undefined {
  return attrs.match(new RegExp(`${name}="([^"]*)"`))?.[1];
}

/** Everything above the closing `---` of the frontmatter, and the blank after. */
export function stripFrontmatter(source: string): string {
  if (!source.startsWith('---\n')) return source;
  const end = source.indexOf('\n---', 3);
  return end === -1 ? source : source.slice(end + 4).replace(/^\n+/, '');
}

export function toMarkdown(source: string, file: string): string {
  const out: string[] = [];
  let inFence = false;
  let quoting = false;

  const emit = (line: string) => out.push(quoting && line.trim() ? `> ${line}` : line);

  for (const raw of source.split('\n')) {
    if (/^\s*(```|~~~)/.test(raw)) {
      inFence = !inFence;
      emit(raw);
      continue;
    }
    if (inFence) {
      out.push(raw);
      continue;
    }

    // An import is how a page would pull in a component this registry already
    // has; in a document it is a line of noise.
    if (/^\s*import\s.+from\s+['"].+['"];?\s*$/.test(raw)) continue;

    const tab = raw.match(/^\s*<Tab\s+value="([^"]*)">\s*$/);
    if (tab) {
      emit(`**${tab[1]}**`);
      emit('');
      continue;
    }
    if (/^\s*<\/Tab>\s*$/.test(raw)) {
      emit('');
      continue;
    }
    if (/^\s*<Tabs\b/.test(raw) || /^\s*<\/Tabs>\s*$/.test(raw)) {
      // The labels are the `<Tab>` headings; the container is chrome.
      if (/^\s*<Tabs\b/.test(raw) && !tabLabels(raw).length) {
        throw new Error(`${file}: <Tabs> is written without an items={[…]} list`);
      }
      emit('');
      continue;
    }
    if (/^\s*<Steps>\s*$/.test(raw) || /^\s*<\/Steps>\s*$/.test(raw)) continue;
    if (/^\s*<Step>\s*$/.test(raw) || /^\s*<\/Step>\s*$/.test(raw)) {
      emit('');
      continue;
    }
    if (/^\s*<Files>\s*$/.test(raw) || /^\s*<\/Files>\s*$/.test(raw)) continue;
    const folder = raw.match(/^\s*<Folder\s+name="([^"]*)">\s*$/);
    if (folder) {
      emit(`**${folder[1]}/**`);
      emit('');
      continue;
    }
    if (/^\s*<\/Folder>\s*$/.test(raw)) continue;
    const fileTag = raw.match(/^\s*<File\s+name="([^"]*)"\s*\/>\s*$/);
    if (fileTag) {
      emit(`- \`${fileTag[1]}\``);
      continue;
    }
    const callout = raw.match(/^\s*<Callout\b([^>]*)>\s*$/);
    if (callout) {
      const title = attr(callout[1], 'title');
      if (title) {
        emit(`**${title}**`);
        emit('');
      }
      quoting = true;
      continue;
    }
    if (/^\s*<\/Callout>\s*$/.test(raw)) {
      quoting = false;
      emit('');
      continue;
    }

    emit(raw);
  }

  const md = collapseBlankLines(out.join('\n'));
  const lines = md.split('\n');
  const leftover = lines.findIndex((l) => /^\s*<\/?[A-Z][A-Za-z]*[\s/>]/.test(l));
  if (leftover !== -1) {
    throw new Error(
      `${file}: ${lines[leftover].trim()} has no case in lib/mdx-to-markdown.ts — add one, ` +
        'or the document ships JSX to whatever agent fetches it',
    );
  }
  return md;
}

/** One blank line between blocks, and no trailing whitespace — outside fences. */
function collapseBlankLines(text: string): string {
  const out: string[] = [];
  let inFence = false;
  let blanks = 0;
  for (const line of text.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    if (!inFence && !line.trim()) {
      blanks++;
      if (blanks > 1) continue;
    } else {
      blanks = 0;
    }
    out.push(inFence ? line : line.replace(/\s+$/, ''));
  }
  return `${out.join('\n').replace(/\n{3,}/g, '\n\n').replace(/^\n+/, '').replace(/\s+$/, '')}\n`;
}
