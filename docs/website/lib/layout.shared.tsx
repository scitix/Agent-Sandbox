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

import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import Image from 'next/image';
import { publicAsset } from './public-assets';

export function baseOptions(): BaseLayoutProps {
  return {
    githubUrl: 'https://github.com/scitix/Agent-Sandbox',
    nav: {
      title: (
        <span className="flex items-center gap-1.5">
          <Image src={publicAsset('/ScitiX.svg')} alt="Agent Sandbox" width={20} height={20} className="shrink-0" />
          <span className="home-display font-bold text-sm tracking-tight">Agent Sandbox</span>
        </span>
      ),
    },
    links: [
      { url: '/docs', text: 'Documentation' },
      { url: '/docs/api/sandboxes/CreateSandbox/', text: 'API Reference' },
    ]
  };
}
