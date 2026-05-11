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

import type { Metadata } from 'next';
import { HomePageClient } from '@/components/home/HomePageClient';

const SITE_URL = 'https://scitix.github.io/Agent-Sandbox/';

export const metadata: Metadata = {
  title: 'Agent Sandbox — Fast, Multi-Cloud Sandboxes for AI Agents',
  description:
    'Open-source Kubernetes sandbox engine for AI agents. Pre-warmed pools with <60ms allocation, cross-cloud routing, zero-rebuild runtime changes, and E2B-compatible SDKs.',
  keywords: [
    'AI sandbox',
    'Kubernetes sandbox',
    'agent infrastructure',
    'E2B alternative',
    'agentic RL',
    'sandbox as a service',
    'sandbox engine',
    'AI agent runtime',
    'pre-warmed pods',
    'in-place upgrade',
  ],
  openGraph: {
    title: 'Agent Sandbox — Fast Sandboxes for AI Agents',
    description:
      'Open-source Kubernetes sandbox engine. <60ms allocation, any Docker image, multi-cloud.',
    url: SITE_URL,
    siteName: 'Agent Sandbox',
    type: 'website',
    images: [{ url: '/og-image.png', width: 1200, height: 630, alt: 'Agent Sandbox' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Agent Sandbox — Fast Sandboxes for AI Agents',
    description:
      'Open-source Kubernetes sandbox engine. <60ms allocation, any Docker image, multi-cloud.',
    images: ['/og-image.png'],
  },
  alternates: {
    canonical: SITE_URL,
  },
};

export default function HomePage() {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify({
            '@context': 'https://schema.org',
            '@type': 'SoftwareApplication',
            name: 'Agent Sandbox',
            applicationCategory: 'DeveloperApplication',
            operatingSystem: 'Kubernetes',
            description:
              'Open-source Kubernetes sandbox engine for AI agents with pre-warmed pools, cross-cloud routing, and E2B-compatible SDKs.',
            url: SITE_URL,
            offers: {
              '@type': 'Offer',
              price: '0',
              priceCurrency: 'USD',
            },
            sourceOrganization: {
              '@type': 'Organization',
              name: 'ScitiX',
            },
          }),
        }}
      />
      <HomePageClient />
    </>
  );
}