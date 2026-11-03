"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Check, Copy, LockKey, Warning, X } from "@phosphor-icons/react";

export type Tone = "ok" | "fail" | "warn" | "idle";

/** Kanso-style status glyph: an outlined circle, filled when something is wrong. */
export function Glyph({ tone, size = 16 }: { tone: Tone; size?: number }) {
  const common = { width: size, height: size, viewBox: "0 0 16 16", "aria-hidden": true as const, className: "shrink-0" };
  if (tone === "fail") {
    return (
      <svg {...common}>
        <circle cx="8" cy="8" r="7" fill="var(--c-fail)" />
        <path d="M5.4 5.4l5.2 5.2M10.6 5.4l-5.2 5.2" stroke="var(--cell-cream)" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  }
  if (tone === "warn") {
    return (
      <svg {...common}>
        <circle cx="8" cy="8" r="6.5" fill="none" stroke="var(--c-warn)" strokeWidth="1.3" strokeDasharray="2.2 2.4" />
      </svg>
    );
  }
  if (tone === "ok") {
    return (
      <svg {...common}>
        <circle cx="8" cy="8" r="6.7" fill="none" stroke="var(--c-text)" strokeWidth="1.3" />
        <path d="M5 8.2l2 2 4-4.2" stroke="var(--c-text)" strokeWidth="1.4" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }
  return (
    <svg {...common}>
      <circle cx="8" cy="8" r="6.7" fill="none" stroke="var(--c-faint)" strokeWidth="1.3" />
    </svg>
  );
}

export const toneWord = (tone: Tone) => (tone === "fail" ? "Fail" : tone === "ok" ? "Pass" : tone === "warn" ? "Caution" : "N/A");

export function useElementSize<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const r = entry.contentRect;
      setSize({ w: Math.round(r.width), h: Math.round(r.height) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, size] as const;
}

export function Numeral({ value, unit, className = "" }: { value: ReactNode; unit?: string; className?: string }) {
  return (
    <p className={`flex items-end gap-2 ${className}`}>
      <span className="numeral text-[clamp(5rem,9vw,7.5rem)]">{value}</span>
      {unit ? <span className="mb-2 text-xl text-muted">{unit}</span> : null}
    </p>
  );
}

export function RailGroup({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="mt-7">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-[15px] font-semibold text-ink">{title}</h3>
        {aside ? <span className="text-sm text-faint">{aside}</span> : null}
      </div>
      <div className="mt-1">{children}</div>
    </section>
  );
}

/** A list row in the rail: glyph, label, optional second line, right-aligned value. */
export function RailRow({
  tone,
  label,
  sub,
  value,
  selected,
  onClick,
  testId,
}: {
  tone: Tone;
  label: ReactNode;
  sub?: ReactNode;
  value?: ReactNode;
  selected?: boolean;
  onClick?: () => void;
  testId?: string;
}) {
  const body = (
    <>
      <Glyph tone={tone} />
      <span className="min-w-0">
        <span className="block truncate text-[15px] text-ink">{label}</span>
        {sub ? <span className="block truncate text-sm text-faint">{sub}</span> : null}
      </span>
      {value ? <span className="whitespace-nowrap text-[15px] tabular-nums text-muted">{value}</span> : <span />}
    </>
  );
  const cls = `grid min-h-12 w-full grid-cols-[16px_1fr_auto] items-center gap-3 border-b border-line px-1 py-2 text-left ${
    selected ? "bg-cell-dark" : ""
  }`;
  if (!onClick) return <div className={cls}>{body}</div>;
  return (
    <button type="button" onClick={onClick} aria-pressed={selected} data-testid={testId} className={`${cls} transition-colors hover:bg-cell-dark`}>
      {body}
    </button>
  );
}

export function OutlineButton({ children, onClick, href, pressed }: { children: ReactNode; onClick?: () => void; href?: string; pressed?: boolean }) {
  const cls =
    "inline-flex min-h-11 items-center justify-center gap-2 rounded-lg border border-line-strong px-4 text-[15px] text-ink transition-colors hover:bg-cell-dark aria-pressed:bg-cell-dark";
  if (href) return <a href={href} className={cls}>{children}</a>;
  return <button type="button" onClick={onClick} aria-pressed={pressed} className={cls}>{children}</button>;
}

export function Disclosure({ summary, children, tone, open }: { summary: ReactNode; children: ReactNode; tone?: Tone; open?: boolean }) {
  return (
    <details className="group border-b border-line" open={open}>
      <summary className="grid min-h-12 grid-cols-[16px_1fr_auto] items-center gap-3 px-1 py-2 text-[15px] text-ink marker:content-none [&::-webkit-details-marker]:hidden">
        <Glyph tone={tone ?? "idle"} />
        <span>{summary}</span>
        <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
      </summary>
      <div className="pb-4 pl-[28px] text-[15px] leading-[1.6] text-muted">{children}</div>
    </details>
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
      className="mono inline-flex min-h-9 max-w-full items-center gap-1.5 rounded-lg bg-sunk px-2.5 text-[13px] text-muted transition-colors hover:text-ink"
    >
      <span className="truncate">{label ?? value}</span>
      {copied ? <Check size={13} weight="bold" aria-hidden /> : <Copy size={13} weight="bold" aria-hidden />}
    </button>
  );
}

export { LockKey, Warning, X };
