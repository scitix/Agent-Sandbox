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
import { SITE_TITLE, SITE_URL, SITE_DESCRIPTION } from '@/lib/metadata';


export const metadata: Metadata = {
  title: SITE_TITLE,
  description: SITE_DESCRIPTION,
  keywords: [
    'AI sandbox',
    'agent infrastructure',
    'multi-turn RL',
    'agentic RL',
    'E2B alternative',
    'SWE-Bench',
    'LLM sandbox',
    'AI agent runtime',
    'Kubernetes sandbox',
    'sandbox engine',
  ],
  openGraph: {
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    url: SITE_URL,
    siteName: 'Agent Sandbox',
    type: "website",
    locale: 'en-US',
    images: [
      {
        url: 'https://scitix.github.io/Agent-Sandbox/og-image.png',
        width: 1200,
        height: 630,
        alt: 'Agent Sandbox',
      }
    ]
  },
  twitter: {
    card: 'summary_large_image',
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    images: ['https://scitix.github.io/Agent-Sandbox/og-image.png'],
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
            operatingSystem: 'Kubernetes / VirtualMachine',
            description: SITE_DESCRIPTION,
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