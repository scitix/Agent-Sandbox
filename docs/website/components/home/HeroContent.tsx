import Link from 'next/link';
import { ArrowRight } from 'lucide-react';
import { Github } from '@lobehub/icons/es/icons';

/**
 * Server Component — semantic hero content rendered in SSR HTML.
 * Separated from animation logic so crawlers see clean, meaningful markup
 * without the decorative icon reel.
 */
export function HeroContent() {
  return (
    <div className="relative z-10 mx-auto w-full max-w-(--fd-layout-width) px-4">
      <div className="flex min-h-[calc(100svh-var(--fd-nav-height,4rem))] items-center justify-center py-20 md:py-28">
        <div className="mx-auto max-w-5xl text-center">
          <p className="inline-flex rounded-full border border-fd-border bg-fd-background/70 px-4 py-2 text-xs font-semibold tracking-[0.18em] text-fd-muted-foreground shadow-sm">
            Open-Source Kubernetes Sandbox Engine
          </p>
          <h1 className="home-display mt-5 text-5xl font-semibold tracking-[-0.065em] text-fd-foreground md:text-7xl lg:text-8xl">
            Fast, Multi-Cloud<br /><span className="relative inline-block text-[var(--brand)]">Sandboxes</span> for<br />AI
            Agents.
          </h1>
          <p className="mx-auto mt-6 max-w-3xl text-lg leading-8 text-fd-muted-foreground md:text-xl">
            Agent Sandbox enables AI agents to interact with real-world APIs and tools in a safe, isolated environment.
          </p>
          <div className="mt-8 flex flex-wrap justify-center gap-3">
            <Link
              href="/docs"
              className="inline-flex items-center gap-2 rounded-full bg-fd-foreground px-5 py-3 text-sm font-semibold text-fd-background"
            >
              Read the docs
              <ArrowRight className="h-4 w-4" strokeWidth={2} />
            </Link>
            <a
              href="https://github.com/scitix/agent-sandbox"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2 rounded-full border border-fd-border bg-fd-background/70 px-5 py-3 text-sm font-semibold text-fd-foreground"
            >
              <Github size={18} />
              View on GitHub
            </a>
          </div>
        </div>
      </div>
    </div>
  );
}