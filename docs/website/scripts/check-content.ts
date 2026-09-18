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
 * What this site may not contain, checked before the build is allowed to finish.
 *
 * The docs are a public mirror of a platform that runs on internal clusters, so
 * the failure mode is a page that documents the deployment its author happened
 * to be looking at: a console host, a registry, a cluster's IP, a real key in a
 * code block. Prose is written by people, and "use a placeholder" is a rule
 * people forget at 6pm.
 *
 * The rule is an ALLOW-list of hosts, not a deny-list of ours: naming the
 * internal ones here would publish the inventory we are trying not to publish.
 * Cluster and tenant *names* are not caught here — those are caught before
 * publication by the private gitleaks layer, which can name them by design.
 *
 * Addresses that are deliberately universal stay: the OSS package bucket every
 * deployment installs from, the project's own repositories, upstream
 * documentation, and the RFC 2606 / RFC 5737 reserved names and ranges that
 * placeholders should be written with.
 */

import { readdirSync, readFileSync, statSync } from 'fs';
import { join } from 'path';

const contentDir = join(process.cwd(), 'content/docs');

/** Hosts this site may link to, exactly. */
const ALLOWED_HOSTS = new Set([
  'scitix.github.io',
  'github.com',
  'www.github.com',
  'oss-ap-southeast.scitix.ai',
  // The public registry the charts and images are published to.
  'ghcr.io',
  'e2b.dev',
  'apache.org',
  'www.apache.org',
  'k8s.io',
  'kubernetes.io',
  'python.org',
  'docs.python.org',
  'pypi.org',
  // The worked example for "an API behind a credential" — a public service, and
  // the one every SDK's own docs reach for.
  'api.openai.com',
  'localhost',
  '127.0.0.1',
  // The CLI's own default output names a cluster id; examples use these.
  'example.com',
]);

/**
 * Host suffixes reserved for documentation by RFC 2606 / RFC 6761. Anything
 * under them is a placeholder by construction, so a page may use as many as it
 * likes without inventing a real hostname to explain something.
 */
const ALLOWED_HOST_SUFFIXES = [
  '.example',
  '.example.com',
  '.test',
  '.invalid',
  '.localhost',
  // Kubernetes service DNS: names no deployment, and the namespace and service
  // in front of it come from the chart.
  '.svc.cluster.local',
];

/**
 * RFC 5737 ranges, plus loopback: addresses that cannot be anybody's cluster.
 * `0.0.0.0` is allowed by name because the egress syntax spells "everywhere" as
 * `0.0.0.0/0`, and writing that is how a run gets cut off from the internet.
 */
function isReservedAddress(ip: string): boolean {
  return (
    ip === '0.0.0.0' ||
    /^127\./.test(ip) ||
    /^192\.0\.2\./.test(ip) ||
    /^198\.51\.100\./.test(ip) ||
    /^203\.0\.113\./.test(ip)
  );
}

interface Finding {
  file: string;
  line: number;
  text: string;
  why: string;
}

function check(rel: string, source: string, findings: Finding[]) {
  source.split('\n').forEach((text, i) => {
    const line = i + 1;
    const add = (why: string) => findings.push({ file: rel, line, text: text.trim(), why });

    // A key, not the placeholder for one: `agbx_...` is how a page says "your
    // key", and a long token after the prefix is what a real one looks like.
    if (/agbx_[A-Za-z0-9]{6,}/.test(text)) {
      add('looks like a real API key — write `agbx_...`');
    }

    for (const m of text.matchAll(/\b(\d{1,3}(?:\.\d{1,3}){3})\b/g)) {
      if (!isReservedAddress(m[1])) add(`IP address ${m[1]} — use 192.0.2.x (RFC 5737)`);
    }

    for (const m of text.matchAll(/(?:https?|oci):\/\/([A-Za-z0-9._-]+)/g)) {
      const host = m[1];
      // `https://YOUR_CONSOLE/...` and `https://<console>/...` are placeholders
      // and the point of the exercise; only a resolvable-looking host is a leak.
      if (/^[A-Z][A-Z0-9_]*$/.test(host)) continue;
      if (ALLOWED_HOSTS.has(host)) continue;
      if (ALLOWED_HOST_SUFFIXES.some(s => host.endsWith(s) || host === s.slice(1))) continue;
      add(`host ${host} is not on the allow-list — use example.com or a placeholder`);
    }
  });
}

function walk(dir: string, relBase = ''): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const abs = join(dir, entry);
    const rel = relBase ? `${relBase}/${entry}` : entry;
    if (statSync(abs).isDirectory()) out.push(...walk(abs, rel));
    else if (/\.(mdx?|json)$/.test(entry)) out.push(rel);
  }
  return out;
}

function main() {
  const findings: Finding[] = [];
  // meta.json is walked too: a title is content, and it is rendered in the
  // sidebar and in llms.txt like any other prose.
  for (const rel of walk(contentDir)) check(rel, readFileSync(join(contentDir, rel), 'utf8'), findings);

  if (!findings.length) {
    console.log('  content check: clean');
    return;
  }
  for (const f of findings) {
    console.error(`  ${f.file}:${f.line}  ${f.why}\n    ${f.text}`);
  }
  console.error(`\n${findings.length} finding(s) — the docs site is public.`);
  process.exit(1);
}

main();
