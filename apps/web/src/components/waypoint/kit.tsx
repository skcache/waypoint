"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy } from "@phosphor-icons/react";

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
    <div className="grid grid-cols-[minmax(0,8.5rem)_1fr] gap-x-4 gap-y-0.5 border-b border-line py-3">
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
      className="mono inline-flex min-h-9 max-w-full items-center gap-2 rounded bg-sunk px-3 text-[13px] text-muted transition-colors hover:text-ink"
    >
      <span className="truncate">{label ?? value}</span>
      {copied ? <Check size={13} weight="bold" aria-hidden /> : <Copy size={13} weight="bold" aria-hidden />}
    </button>
  );
}
