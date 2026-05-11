'use client';

import Image from 'next/image';
import Link from 'next/link';
import DottedMap from 'dotted-map';
import { BookOpen, MessageSquareText } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  AlibabaCloud,
  Aws,
  Azure,
  Cloudflare,
  Github,
  GoogleCloud,
  HuggingFace,
  Volcengine,
} from '@lobehub/icons/es/icons';
import type { IconType } from '@lobehub/icons/es/types';
import { publicAsset } from '@/lib/public-assets';
import { HeroSection } from './HeroSection';
import styles from './HomePageClient.module.css';

type BrandIconType = IconType & {
  Color?: IconType;
};

const CORE_SELLING_POINTS = [
  {
    eyebrow: 'Fast Startup',
    title: 'With <60ms sandbox allocation.',
    accent: '60ms',
    copy: 'Pre-warmed pools keep isolated environments ready for agent loops, eval batches, and RL rollouts instead of waiting on a new Pod every request.',
    tags: ['Pre-warmed pools', 'No pod churn', 'High-volume rollouts'],
  },
  {
    eyebrow: 'Kubernetes native',
    title: 'Scale across any clouds.',
    accent: 'any clouds.',
    copy: 'Use the Kubernetes model your infrastructure team already trusts: CRDs, namespaces, RBAC, autoscaling, in-place updates, and multi-cluster routing.',
    tags: ['CRDs + RBAC', 'In-place updates', 'Multi-cluster'],
  },
  {
    eyebrow: 'Agentic RL',
    title: 'Easily run Agentic RL with zero rebuilds.',
    accent: 'zero',
    copy: 'Stay compatible with E2B / SWE-ReX workflows while adding deterministic resets, any-image runtimes, and the scale needed for asynchronous agent training.',
    tags: ['E2B compatible', 'SWE-ReX friendly', 'Any Docker image'],
  },
] as const;

const PRODUCT_SLIDES = [
  {
    title: 'Sandbox lifecycle and pool management',
    copy: 'Manage sandbox runtimes, dataset mounts, image versions, and reset policies without forcing researchers to operate Kubernetes directly.',
    image: publicAsset('/monitor-compressed.webp'),
    darkImage: publicAsset('/monitor-dark-compressed.webp'),
    width: 2048,
    height: 1092,
  },
  {
    title: 'Logs, debugging, and monitoring',
    copy: 'Inspect commands, terminals, logs, metrics, and route traces when an agent task fails or a training batch slows down.',
    image: publicAsset('/terminal-compressed.webp'),
    darkImage: publicAsset('/terminal-dark-compressed.webp'),
    width: 2048,
    height: 1093,
  },
] as const;

const AUTO_SLIDE_INTERVAL_MS = 4000;
const MANUAL_SLIDE_PAUSE_MS = 30000;

const GLOBAL_NODES = [
  { label: 'RL Training', lat: 39.0, lng: -77.0 },
  { label: 'RL Sandbox', lat: 50.1, lng: 8.7 },
  { label: 'RL Sandbox', lat: 35.7, lng: 139.7 },
] as const;

const GLOBAL_ROUTES = [
  { end: GLOBAL_NODES[1], delay: 0 },
  { end: GLOBAL_NODES[2], delay: 0.6 },
] as const;

const SUMMARY_CARDS = [
  {
    eyebrow: 'Speed',
    title: '<60ms allocation',
    accent: '<60ms',
    copy: 'Pre-warmed Pod pools hand requests to idle sandboxes fast enough for high-volume agent loops and RL rollouts.',
  },
  {
    eyebrow: 'Platform',
    title: 'Kubernetes-native adoption',
    accent: 'Kubernetes-native',
    copy: 'Run on the Kubernetes estate your team already operates, with CRDs, namespaces, RBAC, autoscaling, and in-place updates that preserve warm capacity.',
  },
  {
    eyebrow: 'Routing',
    title: 'Cross-region and cross-cloud routing',
    accent: 'cross-cloud',
    copy: 'Dispatch requests across clouds, clusters, and regions without forcing application teams to manage bespoke ingress or routing logic.',
  },
  {
    eyebrow: 'Runtime',
    title: 'Zero rebuild runtime changes',
    accent: 'Zero rebuild',
    copy: 'Use Docker images directly for SWE tasks, RL environments, terminals, and internal tools without rebuilding VM images.',
  },
  {
    eyebrow: 'SDKs',
    title: 'SDKs for agent training',
    accent: 'agent training',
    copy: 'Support E2B-compatible clients, SWE-ReX workflows, and reinforcement learning frameworks with a familiar sandbox API.',
  },
  {
    eyebrow: 'Console',
    title: 'Console-grade observability',
    accent: 'observability',
    copy: 'Give operators a complete view of pools, sessions, logs, metrics, routes, and failures from the product console.',
  },
] as const;

const FOOTER_GROUPS = [
  {
    title: 'Product',
    links: [
      { label: 'Quick Start', href: '/docs' },
      { label: 'Integrations', href: '/docs/integrations' },
      { label: 'API Reference', href: '/docs/api' },
      { label: 'Helm Install', href: '/docs/installation' },
    ],
  },
  {
    title: 'Community',
    links: [
      { label: 'GitHub', href: 'https://github.com/scitix/agent-sandbox' },
      { label: 'Issues', href: 'https://github.com/scitix/agent-sandbox/issues' },
      { label: 'Discussions', href: 'https://github.com/scitix/agent-sandbox/discussions' },
    ],
  },
  {
    title: 'Resources',
    links: [
      { label: 'Changelog', href: '/docs/changelog' },
      { label: 'Contributing', href: '/docs/contributing' },
      { label: 'License', href: '/docs/license' },
    ],
  },
] as const;

const HELM_COMMAND = 'helm install agent-sandbox oci://ghcr.io/scitix/agent-sandbox-worker';

const TERMINAL_LINES = [
  { kind: 'code', text: 'from agentbox.patch_e2b import patch_e2b' },
  { kind: 'code', text: 'patch_e2b()' },
  { kind: 'gap', text: '' },
  { kind: 'code', text: 'from e2b import Sandbox' },
  { kind: 'gap', text: '' },
  { kind: 'info', text: 'Any Docker image — no rebuild needed' },
  { kind: 'code', text: 'sbx = Sandbox.create("my-pool//ubuntu:22.04", timeout=3600)' },
  { kind: 'output', text: 'allocating from pre-warmed pool' },
  { kind: 'success', text: '✓ sandbox-4f8a1c ready in 74ms' },
  { kind: 'gap', text: '' },
  { kind: 'command', text: 'sbx.commands.run("python3 --version").stdout' },
  { kind: 'return', text: "'Python 3.11.9'" },
  { kind: 'gap', text: '' },
  { kind: 'command', text: 'sbx.kill()' },
  { kind: 'success', text: '✓ pod-03 phase: Idle' },
] as const;

const BENCHMARK_DATASETS: readonly {
  label: string;
  href?: string;
  icon?: BrandIconType;
  source: string;
}[] = [
  {
    label: 'SWE-Bench Verified',
    href: 'https://huggingface.co/datasets/princeton-nlp/SWE-bench_Verified',
    icon: HuggingFace,
    source: 'Hugging Face',
  },
  {
    label: 'SWE-Gym',
    href: 'https://huggingface.co/datasets/SWE-Gym/SWE-Gym',
    icon: HuggingFace,
    source: 'Hugging Face',
  },
  {
    label: 'Terminal-Bench',
    href: 'https://github.com/harbor-framework/terminal-bench',
    icon: Github,
    source: 'GitHub',
  },
  {
    label: 'Custom RL environments',
    source: 'Bring your own tasks',
  },
] as const;

function Bound({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`mx-auto w-full max-w-(--fd-layout-width) px-4 ${className}`}>{children}</div>;
}

function SectionIntro({
  title,
  copy,
  accent,
  align = 'left',
}: {
  title: string;
  copy: string;
  accent?: string | readonly string[];
  align?: 'left' | 'center';
}) {
  const accents = Array.isArray(accent) ? accent : accent ? [accent] : [];
  const titleParts = accents.length > 0 ? title.split(new RegExp(`(${accents.map(escapeRegExp).join('|')})`, 'g')) : [title];

  return (
    <div className={align === 'center' ? 'mx-auto max-w-3xl text-center' : 'max-w-3xl'}>
      <h2 className="home-display text-3xl font-semibold leading-[1.12] tracking-[-0.04em] text-fd-foreground md:text-5xl md:leading-[1.08]">
        {titleParts.map((part, index) =>
          accents.includes(part) ? (
            <span key={`${part}-${index}`} className={`${styles.sectionAccent} relative inline-block`}>
              {part}
            </span>
          ) : (
            part
          ),
        )}
      </h2>
      <p className="mt-4 text-base leading-8 text-fd-muted-foreground md:text-lg">{copy}</p>
    </div>
  );
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function Reveal({
  children,
  className = '',
  delay = 0,
}: {
  children: React.ReactNode;
  className?: string;
  delay?: number;
}) {
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y: 22 }}
      viewport={{ once: true, margin: '-80px' }}
      transition={{ delay, duration: 0.55, ease: [0.22, 1, 0.36, 1] }}
      whileInView={{ opacity: 1, y: 0 }}
    >
      {children}
    </motion.div>
  );
}

function TextLink({ href, children }: { href: string; children: React.ReactNode }) {
  const isExternal = href.startsWith('http');

  if (isExternal) {
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" className="text-fd-muted-foreground hover:text-fd-foreground">
        {children}
      </a>
    );
  }

  return (
    <Link href={href} className="text-fd-muted-foreground hover:text-fd-foreground">
      {children}
    </Link>
  );
}

function CloudIconStrip() {
  const providers = [
    { label: 'AWS', icon: Aws },
    { label: 'Google Cloud', icon: GoogleCloud },
    { label: 'Azure', icon: Azure },
    { label: 'Volcengine', icon: Volcengine },
    { label: 'Alibaba Cloud', icon: AlibabaCloud },
    { label: 'Cloudflare', icon: Cloudflare },
  ];

  return (
    <div className="mt-8 flex flex-wrap gap-3">
      {providers.map((provider) => (
        <motion.div
          key={provider.label}
          className={`${styles.pill} flex items-center gap-2 rounded-full px-3 py-2 text-sm text-fd-muted-foreground`}
          whileHover={{ y: -2 }}
          transition={{ duration: 0.18 }}
        >
          <provider.icon.Color size={18} />
          <span>{provider.label}</span>
        </motion.div>
      ))}
    </div>
  );
}

function CoreSellingPointsSection() {
  const highlight = (title: string, accent: string) => {
    const [before, after] = title.split(accent);

    if (after === undefined) {
      return title;
    }

    return (
      <>
        {before}
        <span className="text-[var(--brand)]">{accent}</span>
        {after}
      </>
    );
  };

  return (
    <section className={`${styles.section} py-16 md:py-24`}>
      <Bound className="relative z-10">
        <div className="grid gap-4 lg:grid-cols-3">
          {CORE_SELLING_POINTS.map((item, index) => (
            <Reveal key={item.title} className="h-full" delay={index * 0.08}>
              <article className={`${styles.featureCard} ${styles.spotCard} h-full rounded-[2rem] p-6 md:p-7`}>
                <div className={styles.brandOrb} />
                <div className="relative flex h-full flex-col">
                  <div className="flex flex-1 flex-col">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--brand)]">{item.eyebrow}</p>
                    <h3 className="home-display mt-5 text-3xl font-semibold leading-[1.02] tracking-[-0.055em] text-fd-foreground">
                      {highlight(item.title, item.accent)}
                    </h3>
                    <p className="mt-5 flex-1 text-sm leading-7 text-fd-muted-foreground">{item.copy}</p>
                  </div>
                  <div className="mt-auto flex flex-wrap gap-2 pt-7">
                    {item.tags.map((tag) => (
                      <span key={tag} className={`${styles.pill} rounded-full px-3 py-1.5 text-xs font-medium text-fd-muted-foreground`}>
                        {tag}
                      </span>
                    ))}
                  </div>
                </div>
              </article>
            </Reveal>
          ))}
        </div>
      </Bound>
    </section>
  );
}

function ProductCarouselSection({
  activeSlide,
  slideDirection,
  onSelect,
  onPrev,
  onNext,
}: {
  activeSlide: number;
  slideDirection: number;
  onSelect: (index: number) => void;
  onPrev: () => void;
  onNext: () => void;
}) {
  const slide = PRODUCT_SLIDES[activeSlide];
  const slideMotion = {
    initial: (direction: number) => ({
      opacity: 0,
      x: direction === 0 ? 0 : direction > 0 ? 28 : -28,
      scale: 0.992,
    }),
    animate: {
      opacity: 1,
      x: 0,
      scale: 1,
    },
    exit: (direction: number) => ({
      opacity: 0,
      x: direction === 0 ? 0 : direction > 0 ? -24 : 24,
      scale: 0.996,
    }),
  };

  return (
    <section className={`${styles.section} py-16 md:py-24`}>
      <Bound className="relative z-10">
        <Reveal>
          <SectionIntro
            align="center"
            title="A complete console for sandbox operations."
            accent="console"
            copy="Manage preheated sandboxes, runtime images, datasets, logs, and monitoring from a single control plane built for agent infrastructure."
          />
        </Reveal>

        <Reveal className="relative mx-auto mt-10 max-w-7xl px-14 md:px-18" delay={0.08}>
          <button
            type="button"
            onClick={onPrev}
            className={`${styles.pill} absolute left-0 top-1/2 z-10 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full text-fd-foreground shadow-sm`}
            aria-label="Previous console screenshot"
          >
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth={1.8}>
              <path d="M15 5l-7 7 7 7" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
          <button
            type="button"
            onClick={onNext}
            className={`${styles.pill} absolute right-0 top-1/2 z-10 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full text-fd-foreground shadow-sm`}
            aria-label="Next console screenshot"
          >
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth={1.8}>
              <path d="M9 5l7 7-7 7" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>

          <div className={`${styles.consoleShell} rounded-[2rem] border border-fd-border p-4 shadow-2xl shadow-fd-foreground/5 md:p-6`}>
            <div className={`${styles.carouselFrame} aspect-[16/9] rounded-[1.5rem] border border-fd-border bg-fd-muted/30`}>
              <AnimatePresence mode="popLayout" initial={false} custom={slideDirection}>
                <motion.div
                  key={activeSlide}
                  className={styles.carouselSlide}
                  custom={slideDirection}
                  variants={slideMotion}
                  initial="initial"
                  animate="animate"
                  exit="exit"
                  transition={{ duration: 0.62, ease: [0.22, 1, 0.36, 1] }}
                >
                  <Image
                    src={slide.image}
                    alt={`${slide.title} console screenshot`}
                    width={slide.width}
                    height={slide.height}
                    sizes="(min-width: 1280px) 1152px, (min-width: 768px) calc(100vw - 9rem), calc(100vw - 7rem)"
                    className="h-auto w-full object-top dark:hidden"
                    priority={activeSlide === 0}
                  />
                  <Image
                    src={slide.darkImage}
                    alt=""
                    width={slide.width}
                    height={slide.height}
                    sizes="(min-width: 1280px) 1152px, (min-width: 768px) calc(100vw - 9rem), calc(100vw - 7rem)"
                    className="hidden h-auto w-full object-top dark:block"
                    priority={activeSlide === 0}
                  />
                  <div className={styles.carouselScrim} aria-hidden="true" />
                  <motion.div
                    className={styles.carouselCaption}
                    initial={{ opacity: 0, y: 14 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 8 }}
                    transition={{ duration: 0.42, ease: [0.22, 1, 0.36, 1], delay: 0.08 }}
                  >
                    <h3 className="home-display mt-3 text-xl font-semibold tracking-[-0.04em] text-fd-foreground md:text-3xl">
                      {slide.title}
                    </h3>
                    <p className="mt-4 max-w-2xl text-sm leading-6 text-fd-muted-foreground md:text-base md:leading-7">{slide.copy}</p>
                  </motion.div>
                </motion.div>
              </AnimatePresence>
            </div>

            <div className="mt-5 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div className="flex gap-2">
                {PRODUCT_SLIDES.map((item, index) => (
                  <button
                    key={item.title}
                    type="button"
                    onClick={() => onSelect(index)}
                    className={`h-2.5 rounded-full transition-all duration-300 ${activeSlide === index ? 'w-10 bg-[var(--brand)]' : 'w-2.5 bg-fd-border'}`}
                    aria-label={`Show ${item.title}`}
                  />
                ))}
              </div>
            </div>
          </div>
        </Reveal>
      </Bound>
    </section>
  );
}

function WorldConnectionMap() {
  const svgMap = useMemo(() => {
    const map = new DottedMap({ height: 68, grid: 'diagonal', projection: { name: 'equirectangular' } });

    return map.getSVG({
      backgroundColor: 'transparent',
      color: 'currentColor',
      radius: 0.22,
      shape: 'circle',
    });
  }, []);

  const project = (lat: number, lng: number) => ({
    x: ((lng + 180) / 360) * 180,
    y: ((90 - lat) / 180) * 68,
  });
  const hub = GLOBAL_NODES[0];
  const hubPoint = project(hub.lat, hub.lng);
  const makeCurve = (end: (typeof GLOBAL_NODES)[number]) => {
    const endPoint = project(end.lat, end.lng);
    const midX = (hubPoint.x + endPoint.x) / 2;
    const lift = Math.min(18, Math.abs(endPoint.x - hubPoint.x) * 0.18 + 6);

    return {
      d: `M ${hubPoint.x} ${hubPoint.y} Q ${midX} ${Math.min(hubPoint.y, endPoint.y) - lift} ${endPoint.x} ${endPoint.y}`,
      endPoint,
    };
  };

  return (
    <div className={`${styles.mapGlow} relative rounded-[2rem] p-2`}>
      <div className="relative aspect-[180/68] overflow-hidden rounded-[1.5rem]">
        <div
          className="absolute inset-0 text-fd-muted-foreground opacity-50 [&_svg]:h-full [&_svg]:w-full"
          aria-hidden="true"
          dangerouslySetInnerHTML={{ __html: svgMap }}
        />

        <svg viewBox="0 0 180 68" className="absolute inset-0 h-full w-full" aria-hidden="true">
          <defs>
            <linearGradient id="world-route-gradient" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="var(--brand)" stopOpacity="0.2" />
              <stop offset="45%" stopColor="var(--brand)" stopOpacity="0.9" />
              <stop offset="100%" stopColor="#00add8" stopOpacity="0.8" />
            </linearGradient>
          </defs>
          {GLOBAL_ROUTES.map((route) => (
            <g key={`${route.end.label}-${route.end.lat}-${route.end.lng}`}>
              {(() => {
                const { d } = makeCurve(route.end);

                return (
                  <>
                    <path d={d} fill="none" stroke="url(#world-route-gradient)" strokeWidth="0.6" strokeLinecap="round" opacity="0.28" />
                    <motion.path
                      d={d}
                      fill="none"
                      stroke="url(#world-route-gradient)"
                      strokeDasharray="5 36"
                      strokeLinecap="round"
                      strokeWidth="0.9"
                      initial={{ strokeDashoffset: 0 }}
                      animate={{ strokeDashoffset: -41 }}
                      transition={{
                        delay: route.delay,
                        duration: 3.5,
                        ease: 'linear',
                        repeat: Infinity,
                      }}
                    />
                  </>
                );
              })()}
            </g>
          ))}
        </svg>

        {GLOBAL_NODES.map((node) => (
          <div
            key={`${node.label}-${node.lng}`}
            className="absolute -translate-x-1/2 -translate-y-1/2 rounded-full border border-fd-border bg-fd-background/92 px-3 py-1 text-xs font-semibold text-fd-foreground shadow-sm"
            style={{
              left: `${(project(node.lat, node.lng).x / 180) * 100}%`,
              top: `${(project(node.lat, node.lng).y / 68) * 100}%`,
            }}
          >
            {node.label}
          </div>
        ))}
      </div>
    </div>
  );
}

function TerminalDemo() {
  const lineClass = {
    code: 'text-sky-100',
    command: 'text-orange-200',
    gap: '',
    info: 'text-white/50',
    output: 'text-white/58',
    return: 'text-emerald-200',
    success: 'text-[var(--brand-hover)]',
  } as const;

  return (
    <div className={`${styles.terminal} overflow-hidden rounded-[2rem] border border-white/10 shadow-2xl`} aria-hidden="true">
      <div className={`${styles.terminalChrome} flex items-center justify-between border-b border-white/10 px-5 py-3`}>
        <div className="flex items-center gap-2">
          <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
          <span className="h-3 w-3 rounded-full bg-[#ffbd2e]" />
          <span className="h-3 w-3 rounded-full bg-[#28c840]" />
        </div>
        <div className="text-xs font-semibold uppercase tracking-[0.16em] text-white/62">e2b-compatible.py</div>
      </div>
      <div className="overflow-x-auto p-5">
        <pre className="min-w-[640px] text-sm leading-7">
          <code>
            {TERMINAL_LINES.map((line, index) => {
              if (line.kind === 'gap') {
                return <span key={index} className="block h-3" />;
              }

              return (
                <motion.span
                  key={`${line.text}-${index}`}
                  className={`block whitespace-pre ${lineClass[line.kind]}`}
                  initial={{ opacity: 0, y: 6 }}
                  transition={{ delay: index * 0.1, duration: 0.5, ease: 'easeOut' }}
                  viewport={{ once: true }}
                  whileInView={{ opacity: 1, y: 0 }}
                >
                  {line.kind === 'command' ? <span className="text-white/38">&gt; </span> : null}
                  {line.text}
                </motion.span>
              );
            })}
          </code>
        </pre>
      </div>
    </div>
  );
}

function GlobalConnectionSection() {
  return (
    <section className={`${styles.section} py-16 md:py-24`}>
      <Bound className="relative z-10">
        <div className="grid gap-10 lg:grid-cols-[1.15fr_0.85fr] lg:items-center">
          <Reveal>
            <WorldConnectionMap />
          </Reveal>

          <Reveal delay={0.1}>
            <SectionIntro
              title="Connect to any cloud."
              accent="any cloud"
              copy="AI infrastructure is rarely balanced: GPU training clusters are expensive and scarce, while the CPU-heavy sandbox work around them often belongs somewhere else. Agent Sandbox routes execution environments to the right Kubernetes pool without forcing training teams to rebuild their stack."
            />
            <CloudIconStrip />
          </Reveal>
        </div>
      </Bound>
    </section>
  );
}

function CodeExampleSection() {
  return (
    <section className={`${styles.section} py-16 md:py-24`}>
      <Bound className="relative z-10">
        <div className="grid gap-10 lg:grid-cols-[0.85fr_1.15fr] lg:items-start">
          <Reveal>
            <div>
              <SectionIntro
                title="Compatible with E2B-style agent workflows."
                accent="E2B-style"
                copy="Keep the sandbox programming model your team already understands while running the backend on Kubernetes. Agent Sandbox is designed for coding agents, evaluation harnesses, and RL pipelines that need fast, repeatable environments."
              />
              <div className="mt-8 grid gap-3 sm:grid-cols-2">
                {BENCHMARK_DATASETS.map((dataset) => {
                  const Icon = dataset.icon ? dataset.icon.Color ?? dataset.icon : null;
                  const content = (
                    <>
                      <span className="flex min-w-0 items-center gap-3">
                        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-fd-background/70">
                          {Icon ? (
                            <Icon size={21} />
                          ) : (
                            <svg viewBox="0 0 24 24" className="h-5 w-5 text-[var(--brand)]" fill="none" stroke="currentColor" strokeWidth={1.8}>
                              <path d="M12 3.5l7 4v8l-7 5-7-5v-8z" />
                              <path d="M8.5 10.5h7M8.5 14h4.5" strokeLinecap="round" />
                            </svg>
                          )}
                        </span>
                        <span className="min-w-0">
                          <span className="block truncate">{dataset.label}</span>
                          <span className="mt-0.5 block text-xs text-fd-muted-foreground">{dataset.source}</span>
                        </span>
                      </span>
                      {dataset.href ? (
                        <svg
                          viewBox="0 0 24 24"
                          className="h-4 w-4 shrink-0 text-fd-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth={1.8}
                        >
                          <path d="M7 17L17 7M9 7h8v8" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      ) : null}
                    </>
                  );

                  return dataset.href ? (
                    <motion.a
                      key={dataset.label}
                      href={dataset.href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={`${styles.pill} group flex items-center justify-between gap-4 rounded-2xl px-4 py-3 text-left text-sm font-medium text-fd-foreground`}
                      transition={{ duration: 0.18 }}
                      whileHover={{ x: 4, y: -2 }}
                    >
                      {content}
                    </motion.a>
                  ) : (
                    <motion.div
                      key={dataset.label}
                      className={`${styles.pill} flex items-center justify-between gap-4 rounded-2xl px-4 py-3 text-left text-sm font-medium text-fd-foreground`}
                      transition={{ duration: 0.18 }}
                      whileHover={{ x: 4, y: -2 }}
                    >
                      {content}
                    </motion.div>
                  );
                })}
              </div>
            </div>
          </Reveal>
          <Reveal delay={0.1}>
            <TerminalDemo />
          </Reveal>
        </div>
      </Bound>
    </section>
  );
}

function SummarySection() {
  return (
    <section className={`${styles.section} py-16 md:py-20`}>
      <Bound className="relative z-10">
        <Reveal>
          <SectionIntro
            align="center"
            title="The sandbox backend AI teams can grow into."
            accent="AI teams"
            copy="Start with fast allocation, then scale into multi-cloud routing, direct Docker image runtimes, training-framework SDKs, and console-grade observability without replacing your Kubernetes platform."
          />
        </Reveal>
        <div className="mt-10 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {SUMMARY_CARDS.map((item, index) => (
            <Reveal key={item.title} delay={index * 0.04}>
              <article className={`${styles.summaryCard} ${styles.spotCard} rounded-[1.5rem] p-5`}>
                <div className="relative flex h-full flex-col">
                  <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--brand)]">{item.eyebrow}</p>
                  <h3 className="mt-4 text-xl font-semibold leading-tight tracking-[-0.035em] text-fd-foreground">
                    {item.title.includes(item.accent) ? (
                      <>
                        {item.title.split(item.accent)[0]}
                        <span className="text-[var(--brand)]">{item.accent}</span>
                        {item.title.split(item.accent)[1]}
                      </>
                    ) : (
                      item.title
                    )}
                  </h3>
                  <p className="mt-3 text-sm leading-6 text-fd-muted-foreground">{item.copy}</p>
                </div>
              </article>
            </Reveal>
          ))}
        </div>
      </Bound>
    </section>
  );
}

function HelmInstallSection() {
  const [copied, setCopied] = useState(false);

  const copyCommand = async () => {
    await navigator.clipboard.writeText(HELM_COMMAND);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  };

  return (
    <section className={`${styles.section} py-16 md:py-24`}>
      <Bound className="relative z-10">
        <Reveal className="mx-auto max-w-4xl text-center">
          <SectionIntro
            align="center"
            title="Start with Helm and Kubernetes."
            copy="Start with the Helm chart, then follow the installation guide for CRDs, routing, runtime pools, and production configuration."
          />
          <div className={`${styles.glassCard} ${styles.installPanel} mt-10 rounded-[2rem] p-4`}>
            <div className="relative flex flex-col gap-3 rounded-[1.5rem] border border-fd-border bg-fd-background/70 p-4 md:flex-row md:items-center">
              <code className="flex-1 overflow-x-auto whitespace-nowrap text-left font-mono text-sm">{HELM_COMMAND}</code>
              <button
                type="button"
                onClick={copyCommand}
                className="rounded-full bg-[var(--brand)] px-4 py-2 text-sm font-semibold text-white shadow-lg shadow-orange-500/20 transition-transform hover:-translate-y-0.5"
              >
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
          </div>
          <div className="mt-6 flex flex-wrap justify-center gap-3">
            <Link href="/docs" className="inline-flex items-center gap-2 rounded-full border border-fd-border bg-fd-background/70 px-5 py-3 text-sm font-semibold text-fd-foreground transition-transform hover:-translate-y-0.5">
              <BookOpen className="h-4 w-4" strokeWidth={2} />
              Read the installation guide
            </Link>
            <a
              href="https://github.com/scitix/agent-sandbox/issues"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2 rounded-full border border-fd-border bg-fd-background/70 px-5 py-3 text-sm font-semibold text-fd-foreground transition-transform hover:-translate-y-0.5"
            >
              <MessageSquareText className="h-4 w-4" strokeWidth={2} />
              Contact us
            </a>
          </div>
        </Reveal>
      </Bound>
    </section>
  );
}

function Footer() {
  return (
    <footer className={`${styles.footer} py-12`}>
      <Bound>
        <div className="grid gap-10 border-b border-fd-border pb-10 md:grid-cols-[1.2fr_2fr]">
          <div>
            <div className="flex items-center gap-3">
              <Image src={publicAsset('/logo.svg')} alt="" width={24} height={24} />
              <span className="font-semibold text-fd-foreground">Agent Sandbox</span>
            </div>
            <p className="mt-4 max-w-sm text-sm leading-6 text-fd-muted-foreground">
              Kubernetes sandbox infrastructure for AI agents, evals, and Agentic RL workloads.
            </p>
          </div>
          <div className="grid gap-8 sm:grid-cols-3">
            {FOOTER_GROUPS.map((group) => (
              <div key={group.title}>
                <h3 className="text-sm font-semibold text-fd-foreground">{group.title}</h3>
                <div className="mt-4 flex flex-col gap-3 text-sm">
                  {group.links.map((link) => (
                    <TextLink key={link.label} href={link.href}>
                      {link.label}
                    </TextLink>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-3 pt-6 text-sm text-fd-muted-foreground md:flex-row md:items-center md:justify-between">
          <span>© 2026 ScitiX.</span>
          <span>Apache 2.0</span>
        </div>
        <p className="pt-3 text-xs leading-5 text-fd-muted-foreground">
          All product names, logos, and brands are property of their respective owners.
        </p>
      </Bound>
    </footer>
  );
}

export function HomePageClient() {
  const [activeSlide, setActiveSlide] = useState(0);
  const [slideDirection, setSlideDirection] = useState(1);
  const manualPauseUntilRef = useRef(0);

  const pauseAutoSlide = () => {
    manualPauseUntilRef.current = Date.now() + MANUAL_SLIDE_PAUSE_MS;
  };

  const prevSlide = () => {
    pauseAutoSlide();
    setSlideDirection(-1);
    setActiveSlide((current) => (current - 1 + PRODUCT_SLIDES.length) % PRODUCT_SLIDES.length);
  };

  const nextSlide = () => {
    pauseAutoSlide();
    setSlideDirection(1);
    setActiveSlide((current) => (current + 1) % PRODUCT_SLIDES.length);
  };

  const selectSlide = (index: number) => {
    pauseAutoSlide();
    setSlideDirection(index === activeSlide ? 0 : index > activeSlide ? 1 : -1);
    setActiveSlide(index);
  };

  useEffect(() => {
    const interval = window.setInterval(() => {
      if (Date.now() < manualPauseUntilRef.current) {
        return;
      }

      setSlideDirection(1);
      setActiveSlide((current) => (current + 1) % PRODUCT_SLIDES.length);
    }, AUTO_SLIDE_INTERVAL_MS);

    return () => window.clearInterval(interval);
  }, []);

  return (
    <main className={`${styles.page} min-h-screen overflow-x-hidden text-fd-foreground`}>
      <HeroSection />
      <CoreSellingPointsSection />
      <ProductCarouselSection
        activeSlide={activeSlide}
        slideDirection={slideDirection}
        onSelect={selectSlide}
        onPrev={prevSlide}
        onNext={nextSlide}
      />
      <GlobalConnectionSection />
      <CodeExampleSection />
      <SummarySection />
      <HelmInstallSection />
      <Footer />
    </main>
  );
}
