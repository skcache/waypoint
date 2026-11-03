"use client";

import { useEffect, useRef, useState, type ElementType, type ReactNode } from "react";
import { Check, Copy } from "@phosphor-icons/react";

/**
 * Reveal on scroll without hiding content from no-JS or reduced-motion users:
 * the server renders it visible; once mounted, anything not yet on screen is
 * set to "out" and flipped to "in" when it arrives. Pure DOM, no React state.
 */
export function Reveal({
  children,
  className = "",
  kind = "reveal",
  as: Tag = "div",
  delay = 0,
}: {
  children: ReactNode;
  className?: string;
  kind?: "reveal" | "print";
  as?: ElementType;
  delay?: number;
}) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    const outer = ref.current;
    const inner = outer?.firstElementChild as HTMLElement | null;
    if (!outer || !inner || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    inner.dataset.state = "out";
    // Observe the unclipped wrapper: a fully clipped element reports zero visible area and would never reveal.
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          window.setTimeout(() => {
            inner.dataset.state = "in";
          }, delay);
          io.disconnect();
        }
      },
      { threshold: 0.1 },
    );
    io.observe(outer);
    return () => io.disconnect();
  }, [delay]);
  return (
    <Tag ref={ref} className={className}>
      <div className={kind}>{children}</div>
    </Tag>
  );
}

/** Inked rubber-stamp roughness, defined once and referenced by every stamp. */
export function InkDefs() {
  return (
    <svg width="0" height="0" aria-hidden className="absolute">
      <filter id="rough" x="-10%" y="-10%" width="120%" height="120%">
        <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" seed="4" result="n" />
        <feDisplacementMap in="SourceGraphic" in2="n" scale="1.8" />
      </filter>
    </svg>
  );
}

export function Stamp({
  word,
  detail,
  tone,
  on,
  rotate = -7,
  className = "",
}: {
  word: string;
  detail?: string;
  tone: "alarm" | "ink";
  on: boolean;
  rotate?: number;
  className?: string;
}) {
  const color = tone === "alarm" ? "var(--alarm)" : "var(--ink)";
  return (
    <div
      data-stamp={word}
      data-on={on}
      aria-hidden={!on}
      className={`pointer-events-none ${on ? "stamp-slam" : "opacity-0"} ${className}`}
      style={{ ["--r" as string]: `${rotate}deg`, transform: `rotate(${rotate}deg)`, color, mixBlendMode: "multiply", filter: "url(#rough)" }}
    >
      <div className="border-[5px] px-4 py-1.5 text-center" style={{ borderColor: color, borderRadius: 6 }}>
        <div className="poster-caps text-[48px] leading-[0.95]">{word}</div>
        {detail ? <div className="mono mt-1 text-[13px] font-bold uppercase tracking-wide">{detail}</div> : null}
      </div>
    </div>
  );
}

/** A decorative barcode derived from the order id, so every receipt's bars are its own. */
export function Barcode({ value }: { value: string }) {
  const bars: { x: number; w: number }[] = [];
  let x = 0;
  for (let i = 0; i < value.length * 2 && x < 232; i++) {
    const c = value.charCodeAt(i % value.length) + i * 7;
    const w = 1 + (c % 4);
    if (i % 2 === 0) bars.push({ x, w });
    x += w + 1 + ((c >> 2) % 2);
  }
  return (
    <svg viewBox="0 0 240 44" className="h-11 w-full" aria-hidden preserveAspectRatio="none">
      {bars.map((b, i) => (
        <rect key={i} x={b.x} y="0" width={b.w} height="44" fill="currentColor" />
      ))}
    </svg>
  );
}

/** Auditor's tick marks: a pencil tick, a red cross, a dash for not applicable. */
export function Tick({ status, size = 18 }: { status?: string; size?: number }) {
  if (status === "PASS")
    return (
      <svg width={size} height={size} viewBox="0 0 20 20" role="img" aria-label="Pass">
        <path d="M3 11.5 8 16 17.5 4" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  if (status === "FAIL")
    return (
      <svg width={size} height={size} viewBox="0 0 20 20" role="img" aria-label="Fail">
        <path d="M4 4 16 16M16 4 4 16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
      </svg>
    );
  return (
    <svg width={size} height={size} viewBox="0 0 20 20" role="img" aria-label="Not applicable">
      <path d="M5 10h10" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeDasharray="1 4" />
    </svg>
  );
}

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(0,8.5rem)_1fr] gap-x-4 gap-y-0.5 border-b border-line py-2.5">
      <dt className="text-sm text-faint">{label}</dt>
      <dd className="min-w-0 break-words text-[15px] text-ink">{children}</dd>
    </div>
  );
}

export function YesNo({ value }: { value?: boolean }) {
  if (value === undefined) return <span className="text-faint">not recorded</span>;
  return <span>{value ? "yes" : "no"}</span>;
}

export function CopyChip({ value, label }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard
          ?.writeText(value)
          .then(() => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
          })
          .catch(() => setCopied(false));
      }}
      aria-label={copied ? "Copied" : `Copy ${label ?? value}`}
      className="mono inline-flex min-h-9 max-w-full items-center gap-1.5 rounded bg-sunk px-2.5 text-[13px] text-muted transition-colors hover:text-ink"
    >
      <span className="truncate">{label ?? value}</span>
      {copied ? <Check size={13} weight="bold" aria-hidden /> : <Copy size={13} weight="bold" aria-hidden />}
    </button>
  );
}
