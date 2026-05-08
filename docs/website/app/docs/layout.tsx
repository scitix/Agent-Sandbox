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

import { source } from '@/lib/source';
import { DocsLayout } from 'fumadocs-ui/layouts/notebook';
import type { ReactNode } from 'react';
import { baseOptions } from '@/lib/layout.shared';

const apiPages = source.getPages().filter((p) => p.url.startsWith('/docs/api/'));
const docsPages = source.getPages().filter((p) => !p.url.startsWith('/docs/api/'));

const apiUrls = new Set(apiPages.map((p) => p.url));
const docsUrls = new Set(docsPages.map((p) => p.url));
const firstApiUrl = apiPages[0]?.url ?? '/docs';

export default function RootDocsLayout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout
      tree={source.getPageTree()}
      {...baseOptions()}
      tabs={[
        {
          title: 'Docs',
          description: 'Guides & integrations',
          url: '/docs',
          urls: docsUrls,
        },
        {
          title: 'API Reference',
          description: 'OpenAPI reference',
          url: firstApiUrl,
          urls: apiUrls,
        },
      ]}
    >
      {children}
    </DocsLayout>
  );
}
