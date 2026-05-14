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

import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';
import type { CSSProperties, ReactNode } from 'react';
import type { Metadata } from 'next';
import { IBM_Plex_Mono, Manrope, Sora } from 'next/font/google';
import { publicAsset } from '@/lib/public-assets';
import { SITE_DESCRIPTION, SITE_TITLE, SITE_URL } from '@/lib/metadata';

export const metadata: Metadata = {
  metadataBase: SITE_URL,
  title: {
    default: 'Agent Sandbox',
    template: '%s — Agent Sandbox',
  },
  description: SITE_DESCRIPTION,
  icons: { icon: publicAsset('/ScitiX.svg') },
};

const sansFont = Manrope({
  subsets: ['latin'],
  variable: '--font-sans',
  display: 'swap',
  weight: ['400', '500', '600', '700'],
});

const displayFont = Sora({
  subsets: ['latin'],
  variable: '--font-display',
  display: 'swap',
  weight: ['400', '500', '600', '700', '800'],
});

const monoFont = IBM_Plex_Mono({
  subsets: ['latin'],
  variable: '--font-mono-display',
  display: 'swap',
  weight: ['400', '500', '600'],
});

const bodyFontStyle = {
  fontFamily: 'var(--font-sans)',
} as CSSProperties;

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${sansFont.variable} ${displayFont.variable} ${monoFont.variable} flex min-h-screen flex-col`}
        style={bodyFontStyle}
      >
        <RootProvider search={{ options: { type: 'static' } }}>
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
