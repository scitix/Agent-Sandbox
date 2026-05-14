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

import { createOpenAPI } from 'fumadocs-openapi/server';
import fs from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';

// const HIDDEN_TAGS = new Set(['admin']);
const HIDDEN_TAGS = new Set([]) as Set<string>;

function loadFilteredDocument(filePath: string): unknown {
  const raw = fs.readFileSync(filePath, 'utf8');
  const doc = yaml.load(raw) as {
    paths?: Record<string, Record<string, { tags?: string[] }>>;
    tags?: { name: string }[];
  };

  if (doc.paths) {
    for (const [routePath, pathItem] of Object.entries(doc.paths)) {
      for (const [method, op] of Object.entries(pathItem)) {
        if (
          op &&
          typeof op === 'object' &&
          'tags' in op &&
          Array.isArray(op.tags) &&
          op.tags.some((t) => HIDDEN_TAGS.has(t))
        ) {
          delete pathItem[method];
        }
      }
      if (Object.keys(pathItem).length === 0) {
        delete doc.paths[routePath];
      }
    }
  }

  if (Array.isArray(doc.tags)) {
    doc.tags = doc.tags.filter((t) => !HIDDEN_TAGS.has(t.name));
  }

  return doc;
}

const schemaPath = path.resolve(
  process.cwd(),
  '../../pkg/openapi/native/openapi.yaml',
);

export const openapi = createOpenAPI({
  input: () => ({
    [schemaPath]: loadFilteredDocument(schemaPath) as never,
  }),
});
