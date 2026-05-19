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

import Image from 'next/image';
import Link from 'next/link';
import DottedMap from 'dotted-map';
import { ArrowUpRightIcon, BookOpen, MessageSquareText } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
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
import { createDeviconIcon, HeroSection } from './HeroSection';
import styles from './HomePageClient.module.css';
import dockerIcon from 'devicon/icons/docker/docker-plain.svg';

type BrandIconType = IconType & {
  Color?: IconType;
};

const CORE_SELLING_POINTS = [
  {
    eyebrow: 'Lightning Fast',
    title: 'Sub-60ms sandbox allocation.',
    accent: 'Sub-60ms',
    copy: 'Pre-warmed pools keep isolated environments on standby. Eliminate cold-start latency for high-frequency agent loops, evaluations, and RL rollouts.',
    tags: ['Pre-warmed Pools', 'Instant Standby'],
  },
  {
    eyebrow: 'Enterprise Grade',
    title: 'Deploy across any cloud.',
    accent: 'any cloud.',
    copy: 'Leverage the infrastructure your team already trusts. Use native Kubernetes features like CRDs, RBAC, and multi-cluster routing—without vendor lock-in.',
    tags: ['Multi-Cloud', 'No Lock-in', 'RBAC'],
  },
  {
    eyebrow: 'Agentic RL',
    title: 'Built for complex, multi-turn Agentic Reinforcement Learning.',
    accent: 'Agentic',
    copy: 'Power advanced asynchronous agent training with stateful environments, lightning-fast deterministic resets, and any-image runtimes.',
    tags: ['Multi-Turn Ready', 'E2B Compatible', 'Any Docker Image'],
  },
] as const;

const PRODUCT_SLIDES = [
  {
    title: 'Abstract away the infrastructure',
    copy: 'Empower AI researchers to manage dataset mounts, image versions, and reset policies visually—without operating infrastructure directly.',
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
  { label: 'Training', lat: 39.0, lng: -77.0 },
  { label: 'Rollout', lat: 50.1, lng: 8.7 },
  { label: 'Rollout', lat: 35.7, lng: 139.7 },
] as const;

const GLOBAL_ROUTES = [
  { end: GLOBAL_NODES[1], delay: 0 },
  { end: GLOBAL_NODES[2], delay: 0.6 },
] as const;

const SUMMARY_CARDS = [
  {
    eyebrow: 'Speed',
    title: 'Sub-60ms allocation',
    accent: 'Sub-60ms',
    copy: 'Pre-warmed pools deliver idle sandboxes instantly, unblocking high-volume agent loops and multi-turn RL rollouts.',
  },
  {
    eyebrow: 'Infrastructure',
    title: 'Backed by Containers or microVMs',
    accent: 'Containers',
    copy: 'Run securely on your existing estate. Utilize CRDs, namespaces, RBAC, and autoscaling to manage warm capacity efficiently.',
  },
  {
    eyebrow: 'Routing',
    title: 'Cross-region and cross-cloud routing',
    accent: 'cross-cloud',
    copy: 'Dispatch requests across clouds, clusters, and regions without forcing application teams to manage routing logic.',
  },
  {
    eyebrow: 'Runtime',
    title: 'Zero-rebuild runtimes',
    accent: 'Zero-rebuild',
    copy: 'Directly run any Docker image for SWE tasks, RL environments, and internal tools without the hassle of building custom VM images.',
  },
  {
    eyebrow: 'Ecosystem',
    title: 'Drop-in agent SDKs',
    accent: 'agent SDKs',
    copy: 'Seamless compatibility with E2B clients, SWE-ReX workflows, and popular reinforcement learning frameworks.',
  },
  {
    eyebrow: 'Observability',
    title: 'Console-grade visibility',
    accent: 'visibility',
    copy: 'Equip operators with a complete view of pools, active sessions, logs, and metrics through a unified product console.',
  },
] as const;

const FOOTER_GROUPS = [
  {
    title: 'Product',
    links: [
      { label: 'Quick Start', href: '/docs' },
      { label: 'Integrations', href: '/docs/integrations' },
      { label: 'API Reference', href: '/docs/api/sandboxes/CreateSandbox/' },
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
      { label: 'Changelog', href: 'https://github.com/scitix/Agent-Sandbox/releases' },
      { label: 'Contributing', href: 'https://github.com/scitix/Agent-Sandbox?tab=contributing-ov-file' },
      { label: 'License', href: 'https://github.com/scitix/Agent-Sandbox?tab=Apache-2.0-1-ov-file' },
    ],
  },
] as const;

const HELM_COMMAND = 'helm install agent-sandbox oci://ghcr.io/scitix/agent-sandbox-worker';

const TERMINAL_LINES = [
  { kind: 'command', text: 'from agent_sandbox_e2b import patch_e2b' },
  { kind: 'command', text: 'patch_e2b()' },
  { kind: 'gap', text: '' },
  { kind: 'command', text: 'from e2b import Sandbox' },
  { kind: 'gap', text: '' },
  { kind: 'info', text: 'Any Docker image — no rebuild needed' },
  { kind: 'command', text: 'sbx = Sandbox.create("us-east::my-pool//ubuntu:22.04", timeout=3600)' },
  { kind: 'output', text: 'allocating from pre-warmed pool of us-east region' },
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

const DockerIcon = createDeviconIcon(dockerIcon, 'Docker logo');

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

function ScitiXColorIcon({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 800 800" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" className="fill-[#000064] dark:fill-white">
      <path d="m273.74187,78.30124l462.65816,0l-76.96556,152.20156l-279.32446,0l-90.80207,0l-217.06018,0c17.29563,-81.28947 84.7486,-152.20156 201.49411,-152.20156zm15.56607,240.40929l90.80207,0l39.77995,0l105.50336,0c275.86533,0 275.86533,402.98823 0,402.98823l-461.79337,0l76.96556,-152.20156l370.12653,0c61.39949,0 61.39949,-97.72032 0,-97.72032l-90.80207,0l-39.77995,0l-106.36814,0c-116.74552,0 -184.19848,-70.91209 -201.49411,-153.06634l217.06018,0z" />
    </svg>
  );
}

function CloudIconStrip() {
  const providers: { label: string; href: string; icon: ReactNode }[] = [
    { label: 'AWS', href: 'https://aws.amazon.com/', icon: <Aws.Color size={18} /> },
    { label: 'Google Cloud', href: 'https://cloud.google.com/', icon: <GoogleCloud.Color size={18} /> },
    { label: 'Azure', href: 'https://azure.microsoft.com/', icon: <Azure.Color size={18} /> },
    { label: 'Volcengine', href: 'https://www.volcengine.com/', icon: <Volcengine.Color size={18} /> },
    { label: 'Alibaba Cloud', href: 'https://www.alibabacloud.com/', icon: <AlibabaCloud.Color size={18} /> },
    { label: 'Cloudflare', href: 'https://www.cloudflare.com/', icon: <Cloudflare.Color size={18} /> },
    { label: 'ScitiX', href: 'https://scitix.ai/', icon: <ScitiXColorIcon size={18} /> },
  ];

  return (
    <div className="mt-8 flex flex-wrap gap-3">
      {providers.map((provider) => (
        <motion.a
          key={provider.label}
          href={provider.href}
          target="_blank"
          rel="noopener noreferrer"
          className={`${styles.pill} flex items-center gap-2 rounded-full px-3 py-2 text-sm text-fd-muted-foreground`}
          whileHover={{ y: -2 }}
          transition={{ duration: 0.18 }}
        >
          {provider.icon}
          <span>{provider.label}</span>
        </motion.a>
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
              <article className={`${styles.featureCard} bg-fd-background h-full rounded-[2rem] p-6 md:p-7`}>
                <div className="relative flex h-full flex-col">
                  <div className="flex flex-1 flex-col">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--brand)]" aria-hidden="true">{item.eyebrow}</p>
                    <h2 className="home-display mt-5 text-3xl font-semibold leading-[1.02] tracking-[-0.055em] text-fd-foreground">
                      {highlight(item.title, item.accent)}
                    </h2>
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
            title="A unified console for sandbox operations."
            accent="console"
            copy="Manage pre-warmed pools, runtime images, datasets, and logs from a single control plane designed specifically for AI infrastructure."
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
            <div className={`${styles.carouselFrame} aspect-[16/9] rounded-[1rem] border border-fd-border bg-fd-muted/30`}>
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
    <div className={`${styles.terminal} hidden sm:block rounded-[2rem] border border-white/10 shadow-2xl`} aria-hidden="true">
      <div className={`${styles.terminalChrome} flex items-center justify-between border-b border-white/10 px-5 py-3`}>
        <div className="flex items-center gap-2">
          <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
          <span className="h-3 w-3 rounded-full bg-[#ffbd2e]" />
          <span className="h-3 w-3 rounded-full bg-[#28c840]" />
        </div>
        <div className="text-xs font-mono text-white/62 mr-3">e2b-compatible.py</div>
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
                  {line.kind === 'command' ? <span className="text-white/38">&gt;&gt;&gt; </span> : null}
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
              title="Intelligent multi-cloud routing."
              accent="multi-cloud"
              copy="AI compute is often fragmented. Agent Sandbox automatically routes execution workloads to the most available clusters across different clouds and regions."
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
                title="Drop-in compatibility with E2B and popular RL frameworks."
                accent="E2B"
                copy="Keep the sandbox programming model your team already knows. Agent Sandbox is designed for coding agents, evaluation harnesses, and RL pipelines needing fast, repeatable environments."
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
                            <DockerIcon className="size-6 text-fd-muted-foreground" aria-label={`View ${dataset.label} on ${dataset.source}`} />
                          )}
                        </span>
                        <span className="min-w-0">
                          <span className="block truncate">{dataset.label}</span>
                          <span className="mt-0.5 block text-xs text-fd-muted-foreground">{dataset.source}</span>
                        </span>
                      </span>
                      {dataset.href ? (
                        <ArrowUpRightIcon className='size-4 text-fd-muted-foreground' />
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
            title="The sandbox backend your AI team can grow into."
            accent="AI team"
            copy="Start with fast allocation, then seamlessly scale into multi-cloud routing, multi-turn RL frameworks, and console-grade observability."
          />
        </Reveal>
        <div className="mt-10 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {SUMMARY_CARDS.map((item, index) => (
            <Reveal key={item.title} delay={index * 0.04}>
              <article className={`${styles.summaryCard} ${styles.spotCard} rounded-[1.5rem] p-5`}>
                <div className="relative flex h-full flex-col">
                  <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--brand)]" aria-hidden="true">{item.eyebrow}</p>
                  <p className="mt-4 text-xl font-semibold leading-tight tracking-[-0.035em] text-fd-foreground">
                    {item.title.includes(item.accent) ? (
                      <>
                        {item.title.split(item.accent)[0]}
                        <span className="text-[var(--brand)]">{item.accent}</span>
                        {item.title.split(item.accent)[1]}
                      </>
                    ) : (
                      item.title
                    )}
                  </p>
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
            title="Deploy in minutes."
            copy="Start with our Helm chart, then follow the quickstart guide to configure routing, runtime pools, and production-ready environments."
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
              <Image src={publicAsset('/ScitiX.svg')} alt="" width={24} height={24} />
              <span className="font-semibold text-fd-foreground">Agent Sandbox</span>
            </div>
            <p className="mt-4 max-w-sm text-sm leading-6 text-fd-muted-foreground">
              The open-source sandbox engine for AI agents, evals, and multi-turn Agentic RL workloads.
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
