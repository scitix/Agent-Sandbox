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

'use client';

import { useEffect, useId, useState } from 'react';

/**
 * A diagram, drawn in the browser from its own source.
 *
 * Client-side rather than pre-rendered because the source is the point: the
 * page shows a picture, the `.md` twin of the same page carries the `mermaid`
 * fence, and both come from one block of text. A build-time SVG would give the
 * reader the picture and the agent nothing.
 *
 * The library is imported inside the effect, so it is fetched only by the pages
 * that have a diagram, and never during the export.
 */
export function Mermaid({ chart }: { chart: string }) {
  const id = useId().replace(/[^a-zA-Z0-9]/g, '');
  const [host, setHost] = useState<HTMLDivElement | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!host) return;
    let cancelled = false;

    const draw = async () => {
      const mermaid = (await import('mermaid')).default;
      // The site's theme is a class on <html>; a diagram drawn for the wrong
      // one is unreadable rather than merely off-brand, so it is drawn per
      // theme and redrawn when the class flips.
      const dark = document.documentElement.classList.contains('dark');
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        theme: dark ? 'dark' : 'neutral',
      });
      try {
        const { svg } = await mermaid.render(`mermaid-${id}`, chart);
        if (!cancelled) host.innerHTML = svg;
      } catch {
        // A diagram that will not parse must not take the page with it. The
        // source is the fallback, and it is what the `.md` twin shows anyway.
        if (!cancelled) setFailed(true);
      }
    };

    void draw();
    const observer = new MutationObserver(() => void draw());
    observer.observe(document.documentElement, { attributeFilter: ['class'] });
    return () => {
      cancelled = true;
      observer.disconnect();
    };
  }, [chart, host, id]);

  if (failed) return <pre className="my-6 overflow-x-auto text-sm">{chart}</pre>;
  return <div ref={setHost} className="my-6 flex justify-center overflow-x-auto" />;
}
