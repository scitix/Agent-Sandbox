'use client';

import { useEffect, useState } from 'react';
import { motion } from 'motion/react';
import type { ComponentType } from 'react';
import cplusplusIcon from 'devicon/icons/cplusplus/cplusplus-original.svg';
import goIcon from 'devicon/icons/go/go-original.svg';
import pythonIcon from 'devicon/icons/python/python-original.svg';
import rustIcon from 'devicon/icons/rust/rust-original.svg';
import typescriptIcon from 'devicon/icons/typescript/typescript-original.svg';
import bashIcon from 'devicon/icons/bash/bash-original.svg';
import vimIcon from 'devicon/icons/vim/vim-original.svg';
import chromeIcon from 'devicon/icons/chrome/chrome-original.svg';
import {
  Claude,
  ClaudeCode,
  Codex,
  CrewAI,
  Cursor,
  DeepSeek,
  Dify,
  Fireworks,
  Gemini,
  Groq,
  HermesAgent,
  HuggingFace,
  LangChain,
  Manus,
  MCP,
  Minimax,
  Mistral,
  N8n,
  OpenAI,
  OpenClaw,
  OpenRouter,
  Qwen,
  Replicate,
  Tavily,
} from '@lobehub/icons/es/icons';
import { HeroContent } from './HeroContent';
import styles from './HeroSection.module.css';

type IconAsset = string | { src: string };
type ReelIconProps = {
  className?: string;
  size?: number | string;
  title?: string;
};
type ReelIconType = ComponentType<ReelIconProps> & {
  Color?: ComponentType<ReelIconProps>;
};
type DeviconOptions = {
  darkMonochrome?: boolean;
};

type ToolGlyphName = 'search' | 'file' | 'terminal';

type ReelItem =
  | {
    type: 'brand';
    label: string;
    group: string;
    icon: ReelIconType;
  }
  | {
    type: 'custom';
    label: string;
    group: string;
    glyph: ToolGlyphName;
  };

function iconAssetSrc(asset: IconAsset) {
  return typeof asset === 'string' ? asset : asset.src;
}

export function createDeviconIcon(asset: IconAsset, _fallbackTitle: string, options: DeviconOptions = {}): ReelIconType {
  function DeviconIcon({ className, size = 30 }: ReelIconProps) {
    const src = iconAssetSrc(asset);

    if (options.darkMonochrome) {
      const maskStyle = {
        height: size,
        mask: `url("${src}") center / contain no-repeat`,
        width: size,
        WebkitMask: `url("${src}") center / contain no-repeat`,
      };

      return (
        <>
          <img
            src={src}
            alt=""
            width={size}
            height={size}
            className={className ? `${className} dark:hidden` : 'object-contain dark:hidden'}
          />
          <span
            role="presentation"
            className={className ? `${className} hidden bg-white dark:block` : 'hidden bg-white dark:block'}
            style={maskStyle}
          />
        </>
      );
    }

    return (
      <img
        src={src}
        alt=""
        width={size}
        height={size}
        className={className ?? 'object-contain'}
      />
    );
  }

  return DeviconIcon;
}

const CplusplusIcon = createDeviconIcon(cplusplusIcon, 'C++ logo');
const GoIcon = createDeviconIcon(goIcon, 'Go logo');
const PythonIcon = createDeviconIcon(pythonIcon, 'Python logo');
const RustIcon = createDeviconIcon(rustIcon, 'Rust logo', { darkMonochrome: true });
const TypeScriptIcon = createDeviconIcon(typescriptIcon, 'TypeScript logo');
const BashIcon = createDeviconIcon(bashIcon, 'Bash logo', { darkMonochrome: true });
const VimIcon = createDeviconIcon(vimIcon, 'Vim logo');
const ChromeIcon = createDeviconIcon(chromeIcon, 'Chrome logo');

const HERO_REEL_COLUMNS: readonly (readonly ReelItem[])[] = [
  [
    { type: 'brand', label: 'Hermes Agent', group: 'LLM agent', icon: HermesAgent },
    { type: 'brand', label: 'OpenClaw', group: 'LLM agent', icon: OpenClaw },
    { type: 'brand', label: 'Manus', group: 'LLM agent', icon: Manus },
    { type: 'brand', label: 'Claude Code', group: 'LLM agent', icon: ClaudeCode },
    { type: 'brand', label: 'Codex', group: 'LLM agent', icon: Codex },
    { type: 'brand', label: 'Cursor', group: 'coding agent', icon: Cursor },
    { type: 'brand', label: 'CrewAI', group: 'LLM agent', icon: CrewAI },
  ],
  [
    { type: 'brand', label: 'C++', group: 'interpreter', icon: CplusplusIcon },
    { type: 'brand', label: 'Rust', group: 'interpreter', icon: RustIcon },
    { type: 'brand', label: 'Go', group: 'interpreter', icon: GoIcon },
    { type: 'brand', label: 'Python', group: 'interpreter', icon: PythonIcon },
    { type: 'brand', label: 'TypeScript', group: 'interpreter', icon: TypeScriptIcon },
    { type: 'brand', label: 'Bash', group: 'terminal', icon: BashIcon },
  ],
  [
    { type: 'brand', label: 'Fireworks', group: 'inference', icon: Fireworks },
    { type: 'brand', label: 'Groq', group: 'inference', icon: Groq },
    { type: 'brand', label: 'OpenRouter', group: 'router', icon: OpenRouter },
    { type: 'brand', label: 'Replicate', group: 'runtime', icon: Replicate },
    { type: 'brand', label: 'Hugging Face', group: 'models', icon: HuggingFace },
    { type: 'brand', label: 'MCP', group: 'Protocol', icon: MCP },
  ],
  [
    { type: 'brand', label: 'LangChain', group: 'agent tool', icon: LangChain },
    { type: 'brand', label: 'Tavily', group: 'web search', icon: Tavily },
    { type: 'brand', label: 'Dify', group: 'agent tool', icon: Dify },
    { type: 'brand', label: 'n8n', group: 'workflow', icon: N8n },
    { type: 'brand', label: 'File Edit', group: 'tool call', icon: VimIcon },
    { type: 'brand', label: 'Web Search', group: 'tool call', icon: ChromeIcon },
  ],
  [
    { type: 'brand', label: 'Claude', group: 'LLM model', icon: Claude },
    { type: 'brand', label: 'GPT', group: 'LLM model', icon: OpenAI },
    { type: 'brand', label: 'Qwen', group: 'LLM model', icon: Qwen },
    { type: 'brand', label: 'DeepSeek', group: 'LLM model', icon: DeepSeek },
    { type: 'brand', label: 'Minimax', group: 'LLM model', icon: Minimax },
    { type: 'brand', label: 'Gemini', group: 'LLM model', icon: Gemini },
    { type: 'brand', label: 'Mistral', group: 'LLM model', icon: Mistral },
  ],
] as const;

function ToolGlyph({ glyph }: { glyph: ToolGlyphName }) {
  return (
    <svg viewBox="0 0 24 24" className="h-6 w-6 text-white" fill="none" stroke="currentColor" strokeWidth={1.8} aria-hidden="true">
      {glyph === 'search' ? (
        <>
          <circle cx="10.5" cy="10.5" r="5.5" />
          <path d="M15 15l4 4" strokeLinecap="round" />
        </>
      ) : null}
      {glyph === 'file' ? (
        <>
          <path d="M7 3.5h6l4 4V20a1.5 1.5 0 0 1-1.5 1.5h-8A1.5 1.5 0 0 1 6 20V5a1.5 1.5 0 0 1 1-1.5z" />
          <path d="M13 3.5V8h4M9 12h6M9 16h4" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : null}
      {glyph === 'terminal' ? (
        <>
          <rect x="4" y="5" width="16" height="14" rx="2" />
          <path d="M8 10l3 2-3 2M13.5 14H17" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : null}
    </svg>
  );
}

function ReelTile({ item }: { item: ReelItem }) {
  const BrandIcon = item.type === 'brand' ? item.icon.Color ?? item.icon : null;
  const customStyle =
    item.type === 'custom' ? { background: toolColors[item.glyph], borderColor: `${toolColors[item.glyph]}70` } : undefined;

  return (
    <div className="flex min-h-28 items-center gap-4 rounded-[1.6rem] border border-fd-border/80 bg-fd-background/92 px-5 py-5 shadow-sm">
      <div
        className="flex h-16 w-16 shrink-0 items-center justify-center rounded-[1.25rem] border border-fd-border bg-fd-muted/40"
        style={customStyle}
      >
        {BrandIcon ? <BrandIcon size={30} /> : item.type === 'custom' ? <ToolGlyph glyph={item.glyph} /> : null}
      </div>
      <div className="min-w-0">
        <div className="truncate text-[17px] font-semibold text-fd-foreground">{item.label}</div>
        <div className="mt-1 truncate text-[12px] font-medium uppercase tracking-[0.1em] text-fd-muted-foreground">
          {item.group}
        </div>
      </div>
    </div>
  );
}

const toolColors: Record<ToolGlyphName, string> = {
  file: '#475569',
  search: '#0f766e',
  terminal: '#4d6babff',
};

function HeroReel() {
  const columns = [...HERO_REEL_COLUMNS, ...HERO_REEL_COLUMNS];

  return (
    <div className={`${styles.reel} pointer-events-none relative h-full overflow-hidden`} aria-hidden="true">
      <div className="absolute left-1/2 top-1/2 w-[3200px] -translate-x-1/2 -translate-y-1/2 [perspective:2000px]">
        <div className="grid h-[1160px] grid-cols-12 gap-6 [transform:rotateX(45deg)_rotateZ(-30deg)_rotateY(10deg)_scale(1.6)] [transform-style:preserve-3d]">
          {columns.map((items, columnIndex) => (
            <div key={columnIndex} className="overflow-hidden">
              <div
                className={`flex flex-col gap-6 ${columnIndex % 2 === 0 ? styles.reelUp : styles.reelDown}`}
                style={{ animationDuration: `${42 + (columnIndex % 6) * 4}s` }}
              >
                <div className="flex flex-col gap-6">
                  {items.map((item) => (
                    <ReelTile key={`a-${item.label}`} item={item} />
                  ))}
                </div>
                <div className="flex flex-col gap-6">
                  {items.map((item) => (
                    <ReelTile key={`b-${item.label}`} item={item} />
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function HeroSection() {
  const [showReel, setShowReel] = useState(false);

  useEffect(() => {
    if (typeof requestIdleCallback !== 'undefined') {
      const id = requestIdleCallback(() => setShowReel(true));
      return () => cancelIdleCallback(id);
    }
    const id = setTimeout(() => setShowReel(true), 200);
    return () => clearTimeout(id);
  }, []);

  return (
    <section className="relative min-h-[calc(100svh-var(--fd-nav-height,4rem))] overflow-hidden border-b border-fd-border">
      <motion.div
        className="absolute inset-0 blur-[2px]"
        aria-hidden="true"
        initial={{ opacity: 0 }}
        animate={{ opacity: showReel ? 0.75 : 0 }}
        transition={{ duration: 1.5, ease: 'easeOut' }}
      >
        {showReel && <HeroReel />}
      </motion.div>
      <div className="absolute inset-0 bg-fd-background/34" aria-hidden="true" />
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,rgba(255,255,255,0.54)_0%,transparent_54%,var(--color-fd-background)_92%)] dark:bg-[radial-gradient(circle_at_center,rgba(6,17,29,0.36)_0%,transparent_55%,var(--color-fd-background)_94%)]" aria-hidden="true" />
      <HeroContent />
    </section>
  );
}