"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy, Minus, X } from "@phosphor-icons/react";

export const btnPrimary =
  "inline-flex h-12 items-center justify-center gap-2 rounded bg-accent px-5 text-base font-medium text-on-accent transition-opacity hover:opacity-90";
export const btnQuiet =
  "inline-flex min-h-11 items-center justify-center gap-2 rounded px-4 text-base font-medium text-ink underline decoration-line-strong underline-offset-4 transition-colors hover:decoration-ink";

export function Container({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`mx-auto w-full max-w-[1280px] px-5 sm:px-8 lg:px-12 ${className}`}>{children}</div>;
}

export function ActHeading({ title, children }: { title: ReactNode; children?: ReactNode }) {
  return (
    <header className="max-w-[760px]">
      <h2 className="text-balance text-[clamp(2rem,4.2vw,3.25rem)] font-semibold leading-[1.05] tracking-[-0.03em]">{title}</h2>
      {children ? <p className="mt-5 max-w-[56ch] text-pretty text-lg leading-[1.55] text-muted">{children}</p> : null}
    </header>
  );
}

export type Status = "PASS" | "FAIL" | "NOT_APPLICABLE" | string;

export function StatusMark({ status, compact = false }: { status?: Status; compact?: boolean }) {
  if (!status) return <span className="text-faint">–</span>;
  const pass = status === "PASS";
  const fail = status === "FAIL";
  const tone = pass ? "text-ok bg-ok-soft" : fail ? "text-bad bg-bad-soft" : "text-faint bg-sunk";
  const Icon = pass ? Check : fail ? X : Minus;
  const word = pass ? "Pass" : fail ? "Fail" : "N/A";
  return (
    <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[13px] font-semibold ${tone}`}>
      <Icon size={13} weight="bold" aria-hidden />
      {compact ? <span className="sr-only">{word}</span> : word}
    </span>
  );
}

export function VerdictTag({ word }: { word: "VERIFIED" | "UNSAFE" | "INCONCLUSIVE" }) {
  const tone = word === "VERIFIED" ? "bg-ok-soft text-ok" : word === "UNSAFE" ? "bg-bad-soft text-bad" : "bg-warn-soft text-warn";
  const Icon = word === "VERIFIED" ? Check : X;
  return (
    <span className={`inline-flex items-center gap-1.5 rounded px-2 py-1 text-sm font-semibold ${tone}`}>
      <Icon size={14} weight="bold" aria-hidden />
      {word === "VERIFIED" ? "Verified" : word === "UNSAFE" ? "Unsafe" : "Inconclusive"}
    </span>
  );
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
      className="mono inline-flex min-h-11 max-w-full items-center gap-1.5 rounded bg-surface px-2.5 py-1 text-[14px] sm:min-h-9 text-muted transition-colors hover:bg-sunk hover:text-ink"
    >
      <span className="truncate">{label ?? value}</span>
      {copied ? <Check size={13} weight="bold" aria-hidden /> : <Copy size={13} weight="bold" aria-hidden />}
    </button>
  );
}

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(0,9rem)_1fr] gap-x-4 gap-y-1 py-2.5 sm:grid-cols-[11rem_1fr]">
      <dt className="text-[15px] text-faint">{label}</dt>
      <dd className="min-w-0 break-words text-[15px] text-ink">{children}</dd>
    </div>
  );
}

export function YesNo({ value }: { value?: boolean }) {
  if (value === undefined) return <span className="text-faint">not recorded</span>;
  return <span className={value ? "text-ink" : "text-muted"}>{value ? "yes" : "no"}</span>;
}
