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

import Link from 'next/link';
import { useEffect, useRef, useState, useCallback } from 'react';

// ─── Terminal ──────────────────────────────────────────────────────────────────

type LineSpec = {
  delay: number;
  text: string;
  type: 'cmd' | 'output' | 'comment' | 'success' | 'blank';
};

const E2B_LINES: LineSpec[] = [
  { delay: 0, type: 'comment', text: '# Route E2B SDK to Agent Sandbox' },
  { delay: 600, type: 'cmd', text: 'from agentbox.patch_e2b import patch_e2b; patch_e2b()' },
  { delay: 1300, type: 'cmd', text: 'from e2b import Sandbox' },
  { delay: 1900, type: 'blank', text: '' },
  { delay: 2100, type: 'comment', text: '# Any Docker image — no rebuild needed' },
  { delay: 2700, type: 'cmd', text: 'sbx = Sandbox.create("my-pool//ubuntu:22.04", timeout=3600)' },
  { delay: 3200, type: 'output', text: '..  allocating from pre-warmed pool' },
  { delay: 3800, type: 'success', text: '  ✓ sandbox-4f8a1c ready in 74ms' },
  { delay: 4400, type: 'blank', text: '' },
  { delay: 4600, type: 'cmd', text: 'sbx.commands.run("python3 --version").stdout' },
  { delay: 5200, type: 'output', text: "  'Python 3.11.9'" },
  { delay: 5800, type: 'blank', text: '' },
  { delay: 6000, type: 'cmd', text: 'sbx.kill()  # returns pod to pool' },
  { delay: 6600, type: 'success', text: '  ✓ pod-03 phase: Idle' },
];

function TerminalE2B({ className = '' }: { className?: string }) {
  const [visible, setVisible] = useState<number[]>([]);
  const [cursor, setCursor] = useState(true);
  const [started, setStarted] = useState(false);

  useEffect(() => {
    const startDelay = setTimeout(() => {
      setStarted(true);
      const timers: ReturnType<typeof setTimeout>[] = [];
      E2B_LINES.forEach((line, i) => {
        timers.push(setTimeout(() => setVisible(prev => [...prev, i]), line.delay));
      });
      return () => timers.forEach(clearTimeout);
    }, 500);
    const blink = setInterval(() => setCursor(c => !c), 530);
    return () => { clearTimeout(startDelay); clearInterval(blink); };
  }, []);

  return (
    <div className={`relative rounded-xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-[#0d0d14] shadow-2xl shadow-black/10 dark:shadow-black/50 overflow-hidden ${className}`}>
      <div className="flex items-center gap-1.5 px-4 py-2.5 border-b border-slate-100 dark:border-slate-800 bg-slate-50 dark:bg-[#111118]">
        <div className="w-3 h-3 rounded-full bg-[#ff5f57]" />
        <div className="w-3 h-3 rounded-full bg-[#febc2e]" />
        <div className="w-3 h-3 rounded-full bg-[#28c840]" />
        <span className="ml-3 text-xs text-slate-400 dark:text-slate-500 font-mono">agentbox — quickstart</span>
      </div>
      <div className="p-5 font-mono text-[13px] leading-[1.7] min-h-[320px]">
        {E2B_LINES.map((line, i) => (
          <div
            key={i}
            className={`transition-opacity duration-150 ${visible.includes(i) ? 'opacity-100' : 'opacity-0'}`}
          >
            {line.type === 'success' ? (
              <span>
                <span className="text-emerald-500 dark:text-emerald-400">  ✓</span>
                <span className="text-emerald-600/90 dark:text-emerald-300/80">{line.text.slice(3)}</span>
              </span>
            ) : line.type === 'output' ? (
              <span>
                <span className="text-slate-400 dark:text-slate-500">  {line.text.slice(2)}</span>
              </span>
            ) : line.type === 'blank' ? (
              <span>&nbsp;</span>
            ) : line.type === 'comment' ? (
              <span className="text-slate-400 dark:text-slate-500">{line.text}</span>
            ) : (
              <span className="text-slate-800 dark:text-slate-100">{line.text}</span>
            )}
          </div>
        ))}
        {started && visible.length > 0 && (
          <span className={`inline-block w-[7px] h-[14px] bg-emerald-500 dark:bg-emerald-400 align-middle ${cursor ? 'opacity-100' : 'opacity-0'} transition-opacity duration-75`} />
        )}
      </div>
    </div>
  );
}

// ─── Shared pod utilities ──────────────────────────────────────────────────────

type PodState = 'idle' | 'allocating' | 'running' | 'returning';

function podBorderBg(state: PodState) {
  if (state === 'allocating') return 'border-yellow-400 bg-yellow-50 dark:bg-yellow-900/20 text-yellow-600 dark:text-yellow-400';
  if (state === 'running') return 'border-emerald-400 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-600 dark:text-emerald-400 shadow-md shadow-emerald-400/30';
  if (state === 'returning') return 'border-cyan-400 bg-cyan-50 dark:bg-cyan-900/20 text-cyan-600 dark:text-cyan-400';
  return 'border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/40 text-slate-400 dark:text-slate-500';
}
function podLabel(state: PodState) {
  if (state === 'allocating') return 'Starting';
  if (state === 'running') return 'Running';
  if (state === 'returning') return 'Stopping';
  return 'Idle';
}
function PodDot({ state }: { state: PodState }) {
  if (state === 'running') return <div className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />;
  if (state === 'allocating') return <div className="w-2 h-2 rounded-full bg-yellow-400 animate-spin" />;
  if (state === 'returning') return <div className="w-2 h-2 rounded-full bg-cyan-400" />;
  return <div className="w-1.5 h-1.5 rounded-full bg-slate-300 dark:bg-slate-600" />;
}
function PodDotBorder({ state }: { state: PodState }) {
  if (state === 'running') return 'border-emerald-400 bg-emerald-400/20';
  if (state === 'allocating') return 'border-yellow-400 bg-yellow-400/20';
  if (state === 'returning') return 'border-cyan-400 bg-cyan-400/20';
  return 'border-slate-300 dark:border-slate-600 bg-slate-100 dark:bg-slate-700/50';
}

function PodCell({ index, state }: { index: number; state: PodState }) {
  return (
    <div className={`relative flex flex-col items-center justify-center py-2.5 px-1 rounded-lg border-2 font-mono text-[10px] font-medium transition-all duration-500 ${podBorderBg(state)}`}>
      <div className={`w-5 h-5 rounded-full border mb-1 flex items-center justify-center transition-all duration-500 ${PodDotBorder({ state })}`}>
        <PodDot state={state} />
      </div>
      <span>pod-{String(index + 1).padStart(2, '0')}</span>
      <span className={`mt-0.5 text-[9px] transition-colors duration-300 ${state === 'running' ? 'text-emerald-500 dark:text-emerald-400' :
        state === 'allocating' ? 'text-yellow-500 dark:text-yellow-400' :
          state === 'returning' ? 'text-cyan-500 dark:text-cyan-400' :
            'text-slate-400 dark:text-slate-500'
        }`}>{podLabel(state)}</span>
    </div>
  );
}

// ─── Diagram 1: Fast Allocation ────────────────────────────────────────────────

const ALLOC_PODS = 5;
const ALLOC_CYCLE = 4800;

function AllocationDiagram() {
  const [step, setStep] = useState<'idle' | 'req' | 'alloc' | 'run' | 'ret'>('idle');
  const [pods, setPods] = useState<PodState[]>(Array(ALLOC_PODS).fill('idle'));
  const [reqPkt, setReqPkt] = useState(false);
  const [resPkt, setResPkt] = useState(false);
  const [label, setLabel] = useState('');
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const t = useCallback((fn: () => void, ms: number) => {
    timers.current.push(setTimeout(fn, ms));
  }, []);

  useEffect(() => {
    const cycle = () => {
      const pod = Math.floor(Math.random() * ALLOC_PODS);
      t(() => { setStep('req'); setReqPkt(true); setLabel('Sandbox.create()'); }, 0);
      t(() => {
        setStep('alloc'); setReqPkt(false); setLabel('allocating pod…');
        setPods(s => s.map((x, i) => i === pod ? 'allocating' : x));
      }, 900);
      t(() => {
        setStep('run'); setLabel('executing');
        setPods(s => s.map((x, i) => i === pod ? 'running' : x));
      }, 1700);
      t(() => { setStep('ret'); setResPkt(true); setLabel('ready in 74ms'); }, 2900);
      t(() => {
        setStep('idle'); setResPkt(false); setLabel('');
        setPods(Array(ALLOC_PODS).fill('idle'));
      }, 4000);
    };
    const init = setTimeout(() => { cycle(); timers.current.push(setInterval(cycle, ALLOC_CYCLE) as unknown as ReturnType<typeof setTimeout>); }, 600);
    return () => { timers.current.forEach(clearTimeout); clearTimeout(init); };
  }, [t]);

  const apiActive = step === 'alloc' || step === 'run' || step === 'ret';

  return (
    <div className="select-none w-full">
      {/* Agent */}
      <div className="flex justify-center mb-1">
        <div className={`relative flex items-center gap-2 px-4 py-2 rounded-lg border-2 font-mono text-xs font-semibold transition-all duration-300 ${step === 'req' || step === 'ret'
          ? 'border-violet-400 bg-violet-50 dark:bg-violet-900/20 text-violet-700 dark:text-violet-300 shadow-md shadow-violet-400/20'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/60 text-slate-600 dark:text-slate-300'
          }`}>
          <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}><path strokeLinecap="round" strokeLinejoin="round" d="M9.813 15.904L9 18.75l-.813-2.846a4.5 4.5 0 00-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 003.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 003.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 00-3.09 3.09z" /></svg>
          Agent
          {(step === 'req' || step === 'ret') && <span className="absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-violet-400 animate-ping" />}
        </div>
      </div>

      {/* Wire Agent→API — fixed height, label overlaid absolutely */}
      <div className="relative flex justify-center h-10 mb-1">
        <div className="w-px h-full bg-slate-200 dark:bg-slate-700 relative overflow-visible">
          {reqPkt && <div className="absolute w-2 h-2 rounded-full bg-violet-400 -left-[3px] shadow-sm shadow-violet-400/50" style={{ animation: 'pktDown 0.8s cubic-bezier(.4,0,.2,1) forwards' }} />}
          {resPkt && <div className="absolute w-2 h-2 rounded-full bg-emerald-400 -left-[3px] shadow-sm shadow-emerald-400/50" style={{ animation: 'pktUp 0.85s cubic-bezier(.4,0,.2,1) forwards' }} />}
        </div>
        <div className="absolute right-[calc(50%+10px)] top-1/2 -translate-y-1/2 text-[10px] font-mono whitespace-nowrap pointer-events-none">
          <span className={`transition-opacity duration-200 ${label ? 'opacity-100' : 'opacity-0'} ${step === 'req' ? 'text-violet-500 dark:text-violet-400' :
            step === 'ret' ? 'text-emerald-500 dark:text-emerald-400' :
              'text-slate-400'
            }`}>{label || '\u00a0'}</span>
        </div>
      </div>

      {/* Agent Sandbox API */}
      <div className="flex justify-center mb-1">
        <div className={`relative flex items-center gap-2 px-4 py-2 rounded-lg border-2 font-mono text-xs font-semibold transition-all duration-300 ${apiActive
          ? 'border-emerald-400 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 shadow-md shadow-emerald-400/20'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/60 text-slate-600 dark:text-slate-300'
          }`}>
          <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3" /></svg>
          Agent Sandbox API
        </div>
      </div>

      {/* Wire API→Pool — fixed height, permanent label slot */}
      <div className="relative flex justify-center h-8 mb-1">
        <div className="w-px h-full bg-slate-200 dark:bg-slate-700" />
        <div className="absolute inset-0 flex items-center justify-center">
          <span className={`text-[9px] font-mono transition-opacity duration-200 ml-20 ${step === 'alloc' ? 'text-yellow-500 dark:text-yellow-400 opacity-100' :
            step === 'run' ? 'text-emerald-500 dark:text-emerald-400 opacity-100' :
              'opacity-0'
            }`}>
            {step === 'alloc' ? 'claiming idle pod' : step === 'run' ? 'executing' : '\u00a0'}
          </span>
        </div>
      </div>

      {/* Pool */}
      <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50/60 dark:bg-slate-900/40 p-3">
        <div className="text-[9px] font-mono text-slate-400 dark:text-slate-500 mb-2.5 text-center tracking-wider uppercase">Pre-warmed Pool</div>
        <div className="grid grid-cols-5 gap-1.5">
          {Array.from({ length: ALLOC_PODS }, (_, i) => <PodCell key={i} index={i} state={pods[i]} />)}
        </div>
      </div>

      <style>{`
        @keyframes pktDown { from { top: 0 } to { top: 100% } }
        @keyframes pktUp   { from { top: 100% } to { top: -8px } }
      `}</style>
    </div>
  );
}

// ─── Diagram 2: Autoscaler ─────────────────────────────────────────────────────

const SCALE_CYCLE = 6000;

function AutoscalerDiagram() {
  // phases: idle → burst (3 new pods appear) → run (pods running) → drain (pods disappear) → idle
  const [phase, setPhase] = useState<'idle' | 'burst' | 'run' | 'drain'>('idle');
  const [count, setCount] = useState(3); // visible pods
  const [running, setRunning] = useState<number[]>([]);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const t = useCallback((fn: () => void, ms: number) => { timers.current.push(setTimeout(fn, ms)); }, []);

  useEffect(() => {
    const cycle = () => {
      // burst: 3 → 5 pods
      t(() => { setPhase('burst'); setCount(5); }, 0);
      t(() => { setRunning([0, 1, 2, 3, 4]); setPhase('run'); }, 1000);
      // drain: pods finish, scale back down
      t(() => { setRunning([0, 1, 2]); }, 2800);
      t(() => { setPhase('drain'); setRunning([]); }, 3800);
      t(() => { setCount(3); setPhase('idle'); }, 4800);
    };
    const init = setTimeout(() => { cycle(); timers.current.push(setInterval(cycle, SCALE_CYCLE) as unknown as ReturnType<typeof setTimeout>); }, 800);
    return () => { timers.current.forEach(clearTimeout); clearTimeout(init); };
  }, [t]);

  const podState = (i: number): PodState => {
    if (i >= count) return 'idle'; // shouldn't appear
    if (running.includes(i)) return 'running';
    if (phase === 'burst' && i >= 3) return 'allocating';
    return 'idle';
  };

  return (
    <div className="select-none w-full">
      {/* Header bar */}
      <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/60 px-4 py-2.5 mb-3 flex items-center justify-between font-mono text-xs">
        <span className="text-slate-500 dark:text-slate-400">SandboxPool / gpu-pool</span>
        <span className={`flex items-center gap-1.5 transition-colors duration-300 ${phase === 'burst' || phase === 'run' ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-400 dark:text-slate-500'
          }`}>
          <span className={`w-1.5 h-1.5 rounded-full ${phase === 'run' ? 'bg-emerald-400 animate-pulse' : phase === 'burst' ? 'bg-yellow-400' : 'bg-slate-300 dark:bg-slate-600'}`} />
          {count} / 6 pods
        </span>
      </div>

      {/* Metric bar */}
      <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/40 px-4 py-3 mb-3">
        <div className="flex items-center justify-between mb-1.5">
          <span className="text-[10px] font-mono text-slate-400 dark:text-slate-500">pool utilization</span>
          <span className={`text-[10px] font-mono font-semibold transition-colors duration-500 ${phase === 'run' ? 'text-emerald-600 dark:text-emerald-400' :
            phase === 'burst' ? 'text-yellow-600 dark:text-yellow-400' : 'text-slate-400'
            }`}>
            {phase === 'run' ? '100%' : phase === 'burst' ? '60%' : phase === 'drain' ? '40%' : '20%'}
          </span>
        </div>
        <div className="h-1.5 rounded-full bg-slate-200 dark:bg-slate-700 overflow-hidden">
          <div className={`h-full rounded-full transition-all duration-700 ${phase === 'run' ? 'bg-emerald-400 w-full' :
            phase === 'burst' ? 'bg-yellow-400 w-3/5' :
              phase === 'drain' ? 'bg-cyan-400 w-2/5' : 'bg-slate-300 dark:bg-slate-600 w-1/5'
            }`} />
        </div>
      </div>

      {/* Event log */}
      <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-[#0d0d14] px-4 py-2 mb-3 font-mono text-[10px] h-10 flex items-center">
        <span className={`transition-opacity duration-200 ${phase !== 'idle' ? 'opacity-100' : 'opacity-0'} ${phase === 'burst' ? 'text-yellow-500 dark:text-yellow-400' :
          phase === 'run' ? 'text-emerald-500 dark:text-emerald-400' :
            phase === 'drain' ? 'text-cyan-500 dark:text-cyan-400' : ''
          }`}>
          {phase === 'burst' ? '↑ scale-up: +2 pods (demand spike)' :
            phase === 'run' ? '  5 pods running — serving burst' :
              phase === 'drain' ? '↓ scale-down: 2 pods idle >30s' : '\u00a0'}
        </span>
      </div>

      {/* Pod grid */}
      <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50/60 dark:bg-slate-900/40 p-3">
        <div className="text-[9px] font-mono text-slate-400 dark:text-slate-500 mb-2.5 text-center tracking-wider uppercase">Autoscaler-managed Pool</div>
        <div className="grid grid-cols-5 gap-1.5">
          {Array.from({ length: 5 }, (_, i) => (
            <div key={i} className={`transition-all duration-500 ${i < count ? 'opacity-100 scale-100' : 'opacity-0 scale-90'}`}>
              <PodCell index={i} state={i < count ? podState(i) : 'idle'} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ─── Diagram 3: Cross-cluster ──────────────────────────────────────────────────

const XCLUSTER_CYCLE = 5500;

type ClusterState = 'idle' | 'selected' | 'running';

function CrossClusterDiagram() {
  const [active, setActive] = useState<number>(-1);
  const [agentStep, setAgentStep] = useState<'idle' | 'sending' | 'done'>('idle');
  const [routeLabel, setRouteLabel] = useState('');
  const [clStates, setClStates] = useState<ClusterState[]>(['idle', 'idle', 'idle']);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const t = useCallback((fn: () => void, ms: number) => { timers.current.push(setTimeout(fn, ms)); }, []);

  useEffect(() => {
    let turn = 0;
    const cycle = () => {
      const target = turn % 3;
      turn++;
      const labels = ['us-east::gpu-pool', 'eu-west::gpu-pool', 'ap-south::gpu-pool'];
      t(() => { setAgentStep('sending'); setRouteLabel(labels[target]); }, 0);
      t(() => { setActive(target); setClStates(s => s.map((_, i) => i === target ? 'selected' : 'idle')); }, 800);
      t(() => { setClStates(s => s.map((_, i) => i === target ? 'running' : 'idle')); }, 1600);
      t(() => { setAgentStep('done'); }, 2200);
      t(() => { setAgentStep('idle'); setActive(-1); setClStates(['idle', 'idle', 'idle']); setRouteLabel(''); }, 4200);
    };
    const init = setTimeout(() => { cycle(); timers.current.push(setInterval(cycle, XCLUSTER_CYCLE) as unknown as ReturnType<typeof setTimeout>); }, 600);
    return () => { timers.current.forEach(clearTimeout); clearTimeout(init); };
  }, [t]);

  const clusters = [
    { id: 'us-east', flag: '🇺🇸', label: 'US East' },
    { id: 'eu-west', flag: '🇪🇺', label: 'EU West' },
    { id: 'ap-south', flag: '🌏', label: 'AP South' },
  ];

  return (
    <div className="select-none w-full">
      {/* Agent */}
      <div className="flex justify-center mb-1">
        <div className={`relative flex items-center gap-2 px-4 py-2 rounded-lg border-2 font-mono text-xs font-semibold transition-all duration-300 ${agentStep !== 'idle'
          ? 'border-violet-400 bg-violet-50 dark:bg-violet-900/20 text-violet-700 dark:text-violet-300 shadow-md shadow-violet-400/20'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/60 text-slate-600 dark:text-slate-300'
          }`}>
          <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}><path strokeLinecap="round" strokeLinejoin="round" d="M9.813 15.904L9 18.75l-.813-2.846a4.5 4.5 0 00-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 003.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 003.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 00-3.09 3.09z" /></svg>
          Agent
          {agentStep !== 'idle' && <span className="absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-violet-400 animate-ping" />}
        </div>
      </div>

      {/* Wire + route label */}
      <div className="relative flex justify-center h-8 mb-1">
        <div className="w-px h-full bg-slate-200 dark:bg-slate-700" />
        <div className="absolute inset-0 flex items-center justify-center">
          <span className={`text-[9px] font-mono transition-opacity duration-200 ml-36 whitespace-nowrap ${routeLabel ? 'opacity-100 text-violet-500 dark:text-violet-400' : 'opacity-0'}`}>
            {routeLabel || '\u00a0'}
          </span>
        </div>
      </div>

      {/* Gateway */}
      <div className="flex justify-center mb-1">
        <div className={`flex items-center gap-2 px-4 py-2 rounded-lg border-2 font-mono text-xs font-semibold transition-all duration-300 ${agentStep !== 'idle'
          ? 'border-emerald-400 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 shadow-md shadow-emerald-400/20'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/60 text-slate-600 dark:text-slate-300'
          }`}>
          <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918" /></svg>
          Global Gateway
        </div>
      </div>

      {/* Fan-out wires to clusters */}
      <div className="relative h-8 mb-1">
        <svg className="absolute inset-0 w-full h-full overflow-visible" style={{ overflow: 'visible' }}>
          {[0, 1, 2].map(i => {
            const x = i === 0 ? '16.5%' : i === 1 ? '50%' : '83.5%';
            return (
              <line key={i}
                x1="50%" y1="0" x2={x} y2="100%"
                stroke={active === i ? (clStates[i] === 'running' ? '#34d399' : '#a78bfa') : 'currentColor'}
                strokeWidth={active === i ? 2 : 1}
                strokeDasharray={active === i ? 'none' : '3 3'}
                className="text-slate-200 dark:text-slate-700 transition-all duration-300"
              />
            );
          })}
        </svg>
      </div>

      {/* Cluster boxes */}
      <div className="grid grid-cols-3 gap-2">
        {clusters.map((cl, i) => (
          <div key={cl.id} className={`rounded-xl border-2 p-2.5 font-mono text-[10px] text-center transition-all duration-400 ${clStates[i] === 'running' ? 'border-emerald-400 bg-emerald-50 dark:bg-emerald-900/20 shadow-md shadow-emerald-400/20' :
            clStates[i] === 'selected' ? 'border-yellow-400 bg-yellow-50 dark:bg-yellow-900/20' :
              'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/40'
            }`}>
            <div className="text-base mb-1">{cl.flag}</div>
            <div className={`font-semibold transition-colors duration-300 ${clStates[i] === 'running' ? 'text-emerald-600 dark:text-emerald-400' :
              clStates[i] === 'selected' ? 'text-yellow-600 dark:text-yellow-400' :
                'text-slate-500 dark:text-slate-400'
              }`}>{cl.label}</div>
            <div className="mt-1">
              <div className={`inline-flex items-center gap-1 text-[9px] transition-colors duration-300 ${clStates[i] === 'running' ? 'text-emerald-500 dark:text-emerald-400' : 'text-slate-400 dark:text-slate-500'
                }`}>
                <span className={`w-1.5 h-1.5 rounded-full ${clStates[i] === 'running' ? 'bg-emerald-400 animate-pulse' :
                  clStates[i] === 'selected' ? 'bg-yellow-400' :
                    'bg-slate-300 dark:bg-slate-600'
                  }`} />
                {clStates[i] === 'running' ? 'Running' : clStates[i] === 'selected' ? 'Routing' : 'Ready'}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Grid background ──────────────────────────────────────────────────────────

function GridBg() {
  return (
    <div className="absolute inset-0 overflow-hidden pointer-events-none">
      <div className="absolute inset-0 opacity-[0.025] dark:opacity-[0.045]"
        style={{ backgroundImage: `linear-gradient(to right,currentColor 1px,transparent 1px),linear-gradient(to bottom,currentColor 1px,transparent 1px)`, backgroundSize: '40px 40px' }} />
      <div className="absolute inset-0 bg-gradient-to-b from-transparent via-transparent to-white dark:to-[#050508]" />
      <div className="absolute top-1/4 left-1/2 -translate-x-1/2 w-[900px] h-[600px] rounded-full bg-emerald-500/5 dark:bg-emerald-500/8 blur-3xl" />
      <div className="absolute top-2/3 left-1/3 w-[500px] h-[400px] rounded-full bg-cyan-500/4 dark:bg-cyan-500/6 blur-3xl" />
    </div>
  );
}

// ─── Features ─────────────────────────────────────────────────────────────────

const FEATURES = [
  { icon: '⚡', title: 'Zero Scheduling Overhead', desc: 'Pre-warmed pod pools bypass the Kubernetes scheduler entirely. Sandboxes allocate in <100ms — no image pull, no node binding, no cold start.' },
  { icon: '🌐', title: 'Cross-Cluster & Multi-Region', desc: 'Single API gateway routes requests across multiple clusters and regions. Agents run where resources are, not where your API server is.' },
  { icon: '🔌', title: 'E2B SDK & SWE-ReX Compatible', desc: 'Drop-in replacement for E2B SDK and SWE-ReX. Migrate existing agent workloads with zero code changes — same API, faster execution.' },
  { icon: '☸', title: 'Kubernetes Native', desc: 'SandboxPool and SandboxTemplate are first-class Kubernetes CRDs. Fully declarative, GitOps-ready, RBAC-scoped.' },
  { icon: '🧠', title: 'Optimized for RL Training', desc: 'High-throughput pool reuse, parallel episode execution, and deterministic reset semantics — purpose-built for reinforcement learning workloads.' },
  { icon: '📦', title: 'Any Image, No Rebuild', desc: 'Switch runtime images without pod recreation. In-place upgrades keep the pool hot — iterate on your container image without downtime.' },
];

// ─── Performance comparison ────────────────────────────────────────────────────

const PERF_ROWS = [
  { label: 'Sandbox allocation', traditional: '15–60s (schedule + pull)', agentbox: '<100ms (pre-warmed)', highlight: true },
  { label: 'Image update', traditional: 'Drain → delete → recreate', agentbox: 'In-place, zero disruption', highlight: false },
  { label: 'Parallel episodes', traditional: 'N × pod creation cost', agentbox: 'Pool reuse, near-zero marginal', highlight: true },
  { label: 'Execution model', traditional: 'One pod per request', agentbox: 'Container / Fn / MicroVM', highlight: false },
  { label: 'SDK compatibility', traditional: 'Custom tooling required', agentbox: 'E2B, SWE-ReX, native REST', highlight: false },
  { label: 'Cross-cluster', traditional: 'Manual ingress per cluster', agentbox: 'Built-in via Envoy ExtProc', highlight: true },
];

// ─── HOW IT WORKS sections ─────────────────────────────────────────────────────

const HOW_SECTIONS = [
  {
    tag: 'INSTANT ALLOCATION',
    headline: <>No Pod Creation.<br />Just Assignment.</>,
    body: 'Traditional Kubernetes creates a new Pod for every request — paying scheduler negotiation, image pull, and init overhead every time. Agent Sandbox skips all of that. Sandboxes are assigned from a pre-warmed pool in a single label-swap, enabling <100ms allocation with zero cold-start penalty.',
    bullets: [
      { icon: '⚡', text: 'Sub-100ms allocation from pool' },
      { icon: '🚫', text: 'Zero Pod creation overhead' },
      { icon: '📦', text: 'Any Docker image, no rebuild needed' },
    ],
    Diagram: AllocationDiagram,
    reverse: false,
  },
  {
    tag: 'AUTOSCALING',
    headline: <>Scale with Demand.<br />Shrink When Idle.</>,
    body: 'The built-in autoscaler watches pool utilization in real time. When demand spikes, it pre-warms new pods before the queue builds up. When pods sit idle past a configurable threshold, they are reclaimed — keeping your cluster lean between RL rollouts or evaluation batches.',
    bullets: [
      { icon: '📈', text: 'Scale up on demand spikes' },
      { icon: '📉', text: 'Reclaim idle pods automatically' },
      { icon: '⚙️', text: 'Configurable min/max bounds' },
    ],
    Diagram: AutoscalerDiagram,
    reverse: true,
  },
  {
    tag: 'CROSS-CLUSTER',
    headline: <>One API.<br />Every Region.</>,
    body: 'Prefix your pool name with a cluster ID — e.g. us-east::gpu-pool — and Agent Sandbox routes the request through its global gateway to the right cluster transparently. No custom ingress, no per-region SDK config. Your training loop stays cluster-agnostic while workloads land where capacity exists.',
    bullets: [
      { icon: '🌐', text: 'Single endpoint across all clusters' },
      { icon: '🔀', text: 'Transparent cross-region routing' },
      { icon: '🗺️', text: 'cluster-id::pool syntax, zero extra config' },
    ],
    Diagram: CrossClusterDiagram,
    reverse: false,
  },
] as const;

// ─── Intersection-triggered section ───────────────────────────────────────────

function RevealSection({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [vis, setVis] = useState(false);
  useEffect(() => {
    const el = ref.current; if (!el) return;
    const obs = new IntersectionObserver(([e]) => { if (e.isIntersecting) setVis(true); }, { threshold: 0.1 });
    obs.observe(el);
    return () => obs.disconnect();
  }, []);
  return (
    <div ref={ref} className={`transition-all duration-700 ${vis ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-8'} ${className}`}>
      {children}
    </div>
  );
}

// ─── Page ──────────────────────────────────────────────────────────────────────

export default function HomePage() {
  return (
    <main className="flex flex-col min-h-screen overflow-x-hidden">

      {/* ── 1. HERO ─────────────────────────────────────────────────────── */}
      <section className="relative flex flex-col items-center justify-start min-h-screen px-6 pt-20 pb-10">
        <GridBg />

        {/* Badge */}
        <div className="relative mt-4 mb-8 inline-flex items-center gap-2 rounded-full border border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-950/60 px-4 py-1.5 text-xs font-mono text-emerald-700 dark:text-emerald-400">
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
          </span>
          Any Docker Image, Zero Rebuild
        </div>

        {/* Headline */}
        <h1 className="relative max-w-4xl text-4xl md:text-6xl font-bold tracking-tight leading-[1.08] mb-5 text-center">
          <span className="font-mono text-slate-900 dark:text-white">Bring Your Own Image.</span>
          <br />
          <span className="font-mono bg-gradient-to-r from-emerald-500 via-cyan-500 to-emerald-400 bg-clip-text text-transparent">
            We Make it a Sandbox in &lt;100ms.
          </span>
        </h1>

        {/* Sub */}
        <p className="relative max-w-xl text-base text-slate-600 dark:text-slate-400 leading-relaxed mb-10 font-light text-center">
          The Kubernetes Sandbox Engine built for RL Scale.<br className="hidden sm:block" />
          Any environment, instantly.
        </p>

        {/* CTAs */}
        <div className="relative flex flex-wrap gap-3 justify-center mb-14">
          <Link href="/docs" className="group inline-flex items-center gap-2 rounded-lg bg-emerald-500 hover:bg-emerald-400 text-white font-medium px-6 py-3 transition-all duration-200 shadow-lg shadow-emerald-500/25 hover:shadow-emerald-500/40 hover:-translate-y-0.5">
            Get Started
            <svg className="w-4 h-4 group-hover:translate-x-0.5 transition-transform" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}><path strokeLinecap="round" strokeLinejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" /></svg>
          </Link>
          <a href="https://github.com/scitix/agent-sandbox" target="_blank" rel="noopener noreferrer"
            className="inline-flex items-center gap-2 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 hover:bg-slate-50 dark:hover:bg-slate-800/60 text-slate-600 dark:text-slate-400 font-medium px-6 py-3 transition-all duration-200 hover:-translate-y-0.5">
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.477 2 12c0 4.418 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.463-1.11-1.463-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.268 2.75 1.026A9.578 9.578 0 0112 6.836a9.59 9.59 0 012.504.337c1.909-1.294 2.747-1.026 2.747-1.026.546 1.377.202 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.163 22 16.418 22 12c0-5.523-4.477-10-10-10z" /></svg>
            GitHub
          </a>
        </div>

        {/* Terminal */}
        <div className="relative w-full max-w-2xl mx-auto">
          <TerminalE2B />
          <div className="mt-10 flex flex-col items-center gap-2 text-slate-400 dark:text-slate-600 text-xs font-mono animate-bounce">
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" /></svg>
            scroll to explore
          </div>
        </div>
      </section>

      {/* ── 2. HOW IT WORKS — 3 alternating sections ─────────────────────── */}
      {HOW_SECTIONS.map((sec, idx) => (
        <section key={idx} className={`relative px-6 py-24 ${idx === 0 ? 'border-t border-slate-100 dark:border-slate-900' : 'border-t border-slate-100 dark:border-slate-900'}`}>
          <div className="max-w-6xl mx-auto">
            <RevealSection>
              <div className={`grid lg:grid-cols-2 gap-16 items-center ${sec.reverse ? 'lg:flex lg:flex-row-reverse' : ''}`}>

                {/* Copy */}
                <div>
                  <div className="inline-block font-mono text-xs text-emerald-600 dark:text-emerald-500 bg-emerald-50 dark:bg-emerald-950/50 border border-emerald-200 dark:border-emerald-900 rounded px-2 py-1 mb-5">
                    {sec.tag}
                  </div>
                  <h2 className="text-3xl md:text-4xl font-bold font-mono text-slate-900 dark:text-white mb-5 leading-tight">
                    {sec.headline}
                  </h2>
                  <p className="text-sm text-slate-500 dark:text-slate-400 leading-relaxed mb-6 max-w-md">
                    {sec.body}
                  </p>
                  <div className="space-y-3">
                    {sec.bullets.map((b, bi) => (
                      <div key={bi} className="flex items-center gap-3">
                        <span className="w-7 h-7 rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900/60 flex items-center justify-center text-sm flex-shrink-0">{b.icon}</span>
                        <span className="text-sm font-mono text-slate-700 dark:text-slate-300">{b.text}</span>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Diagram */}
                <div className={sec.reverse ? 'lg:pr-8' : 'lg:pl-4'}>
                  <sec.Diagram />
                </div>
              </div>
            </RevealSection>
          </div>
        </section>
      ))}

      {/* ── 3. FEATURES GRID ──────────────────────────────────────────────── */}
      <section className="relative px-6 py-24 border-y border-slate-100 dark:border-slate-900 bg-slate-50/50 dark:bg-slate-950/50">
        <div className="max-w-5xl mx-auto">
          <RevealSection>
            <div className="text-center mb-14">
              <div className="inline-block font-mono text-xs text-cyan-600 dark:text-cyan-500 bg-cyan-50 dark:bg-cyan-950/40 border border-cyan-200 dark:border-cyan-900 rounded px-2 py-1 mb-4">CAPABILITIES</div>
              <h2 className="text-3xl md:text-4xl font-bold font-mono text-slate-900 dark:text-white">Everything your AI agents need.</h2>
            </div>
            <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {FEATURES.map((f, i) => (
                <div key={i} className="group relative p-6 rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-[#0d0d18] hover:border-emerald-300 dark:hover:border-emerald-800 transition-all duration-300 hover:shadow-lg hover:shadow-emerald-500/5 hover:-translate-y-0.5">
                  <div className="text-2xl mb-3">{f.icon}</div>
                  <h3 className="font-mono font-semibold text-slate-900 dark:text-white mb-2 text-sm">{f.title}</h3>
                  <p className="text-sm text-slate-500 dark:text-slate-400 leading-relaxed">{f.desc}</p>
                  <div className="absolute bottom-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-emerald-500/0 to-transparent group-hover:via-emerald-500/30 transition-all duration-500 rounded-b-xl" />
                </div>
              ))}
            </div>
          </RevealSection>
        </div>
      </section>

      {/* ── 4. PERFORMANCE COMPARISON ─────────────────────────────────────── */}
      <section className="relative px-6 py-24">
        <div className="max-w-5xl mx-auto">
          <RevealSection>
            <div className="grid md:grid-cols-2 gap-14 items-start">
              <div>
                <div className="inline-block font-mono text-xs text-slate-500 bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded px-2 py-1 mb-5">PERFORMANCE</div>
                <h2 className="text-3xl md:text-4xl font-bold font-mono text-slate-900 dark:text-white mb-6 leading-tight">Built for RL.<br />Beyond plain Kubernetes.</h2>
                <div className="space-y-4 text-sm text-slate-500 dark:text-slate-400 leading-relaxed">
                  <p>Traditional Kubernetes creates a new Pod for every sandbox request — paying scheduling, image-pull, and init overhead each time. At RL scale with thousands of parallel episodes, this compounds into minutes of lost compute per rollout.</p>
                  <p>Agent Sandbox maintains a pre-warmed pool. Allocation is a label swap, not a pod creation. Supports stateless functions, standard containers, and MicroVMs in the same cluster.</p>
                  <p>Cross-region aggregation via Envoy ExtProc means your training loop sees a single endpoint — no manual ingress for each cluster.</p>
                </div>
                <div className="mt-8">
                  <Link href="/docs" className="inline-flex items-center gap-1.5 text-sm font-medium text-emerald-600 dark:text-emerald-400 hover:text-emerald-500 dark:hover:text-emerald-300 transition-colors font-mono">
                    Architecture deep-dive
                    <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}><path strokeLinecap="round" strokeLinejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" /></svg>
                  </Link>
                </div>
              </div>
              <div className="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-[#0a0a10] overflow-hidden font-mono text-sm">
                <div className="grid grid-cols-3 text-xs border-b border-slate-100 dark:border-slate-800">
                  <div className="px-4 py-3 text-slate-400" />
                  <div className="px-4 py-3 text-slate-500">Traditional K8s</div>
                  <div className="px-4 py-3 text-emerald-600 dark:text-emerald-400">Agent Sandbox</div>
                </div>
                {PERF_ROWS.map((row, i) => (
                  <div key={i} className={`grid grid-cols-3 border-b border-slate-100 dark:border-slate-800/60 last:border-b-0 ${row.highlight ? 'bg-emerald-50/50 dark:bg-emerald-950/20' : ''}`}>
                    <div className="px-4 py-3.5 text-xs text-slate-500 dark:text-slate-400">{row.label}</div>
                    <div className="px-4 py-3.5 text-xs text-slate-400 dark:text-slate-500">{row.traditional}</div>
                    <div className={`px-4 py-3.5 text-xs ${row.highlight ? 'text-emerald-600 dark:text-emerald-300' : 'text-slate-700 dark:text-slate-300'}`}>{row.agentbox}</div>
                  </div>
                ))}
              </div>
            </div>
          </RevealSection>
        </div>
      </section>

      {/* ── CTA ────────────────────────────────────────────────────────────── */}
      <section className="relative px-6 py-20">
        <div className="max-w-3xl mx-auto text-center">
          <div className="relative rounded-2xl border border-emerald-200 dark:border-emerald-900 bg-gradient-to-b from-emerald-50 dark:from-emerald-950/30 to-transparent p-12 overflow-hidden">
            <div className="absolute inset-0 bg-gradient-to-br from-emerald-500/5 via-transparent to-cyan-500/5 pointer-events-none" />
            <div className="absolute inset-0 opacity-[0.04] pointer-events-none" style={{ backgroundImage: `linear-gradient(to right,currentColor 1px,transparent 1px),linear-gradient(to bottom,currentColor 1px,transparent 1px)`, backgroundSize: '24px 24px' }} />
            <h2 className="relative text-3xl md:text-4xl font-bold font-mono text-slate-900 dark:text-white mb-4">Ready to deploy?</h2>
            <p className="relative text-slate-600 dark:text-slate-400 mb-8 leading-relaxed">Deploy Agent Sandbox to your Kubernetes cluster in minutes.<br />Single-YAML installer, zero external dependencies.</p>
            <Link href="/docs" className="relative inline-flex items-center gap-2 rounded-lg bg-emerald-500 hover:bg-emerald-400 text-white font-medium px-6 py-3 transition-all duration-200 shadow-lg shadow-emerald-500/25 hover:shadow-emerald-500/40 hover:-translate-y-0.5 font-mono">
              View Installation Guide →
            </Link>
          </div>
        </div>
      </section>

      {/* ── Footer ─────────────────────────────────────────────────────────── */}
      <footer className="px-6 py-8 border-t border-slate-100 dark:border-slate-900">
        <div className="max-w-5xl mx-auto flex flex-wrap items-center justify-between gap-4 text-xs text-slate-400 dark:text-slate-600 font-mono">
          <span>Agent Sandbox — Kubernetes Operator for AI Agent Sandboxes</span>
          <div className="flex gap-4">
            <Link href="/docs" className="hover:text-slate-600 dark:hover:text-slate-400 transition-colors">Docs</Link>
            <a href="https://github.com/scitix/agent-sandbox" target="_blank" rel="noopener noreferrer" className="hover:text-slate-600 dark:hover:text-slate-400 transition-colors">GitHub</a>
          </div>
        </div>
      </footer>
    </main>
  );
}
