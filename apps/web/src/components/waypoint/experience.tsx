"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { WarningCircle } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { GateLine } from "./gate-line";
import { BreakCanvas, BreakRail, EvidenceCanvas, EvidenceRail, FixCanvas, FixRail, JudgeCanvas, JudgeRail } from "./views";
import { VIEWS, type Candidate, type EvidenceSource, type View } from "./types";

type Variant = "showcase" | "demo";

function subscribe(cb: () => void) {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

function useHashView(): [View, (v: View) => void] {
  const raw = useSyncExternalStore(subscribe, () => window.location.hash.slice(1), () => "");
  const view = (VIEWS.some((v) => v.id === raw) ? raw : "break") as View;
  const go = useCallback((v: View) => {
    window.location.hash = v;
    window.scrollTo({ top: 0 });
  }, []);
  return [view, go];
}

export function Showcase({ initial, variant }: { initial: DemoExport | null; variant: Variant }) {
  const [doc, setDoc] = useState<DemoExport | null>(initial);
  const [source, setSource] = useState<EvidenceSource>("recorded");
  const [loading, setLoading] = useState(!initial);
  const [error, setError] = useState("");
  const [view, go] = useHashView();
  const [candidate, setCandidate] = useState<Candidate>("repair");
  const [inv, setInv] = useState("I13");
  const [mode, setMode] = useState<"map" | "code">("map");
  const [differences, setDifferences] = useState(false);
  const [step, setStep] = useState("judge");
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (bodyRef.current) bodyRef.current.scrollTop = 0;
  }, [view]);

  useEffect(() => {
    let live = true;
    async function load() {
      if (initial) {
        setDoc(initial);
        setLoading(false);
      }
      const forceRecorded = typeof window !== "undefined" && new URLSearchParams(window.location.search).get("source") === "recorded";
      if (variant === "demo" && !forceRecorded) {
        try {
          const res = await fetch("/demo/local.json", { cache: "no-store" });
          if (res.ok) {
            const local = (await res.json()) as DemoExport;
            if (live) {
              setDoc(local);
              setSource("local");
              setLoading(false);
            }
          }
        } catch {
          // The committed recorded evidence remains the safe fallback.
        }
      }
      if (!initial && live) {
        try {
          const res = await fetch("/demo/recorded.json", { cache: "no-store" });
          if (!res.ok) throw new Error("The committed evidence file is unavailable.");
          const recorded = (await res.json()) as DemoExport;
          if (live) {
            setDoc(recorded);
            setSource("recorded");
          }
        } catch (loadError) {
          if (live) setError(loadError instanceof Error ? loadError.message : "Could not load evidence.");
        } finally {
          if (live) setLoading(false);
        }
      }
    }
    void load();
    return () => {
      live = false;
    };
  }, [initial, variant]);

  if (loading && !doc) return <Skeleton />;
  if (!doc) return <EvidenceError message={error} />;

  const rail = { doc, go };
  const head =
    view === "break" ? <BreakRail {...rail} part="head" inv={inv} setInv={setInv} source={source} /> :
    view === "fix" ? <FixRail {...rail} part="head" setCandidate={setCandidate} /> :
    view === "judge" ? <JudgeRail {...rail} part="head" candidate={candidate} setCandidate={setCandidate} inv={inv} mode={mode} setMode={setMode} differences={differences} setDifferences={setDifferences} /> :
    <EvidenceRail {...rail} part="head" step={step} setStep={setStep} />;
  const body =
    view === "break" ? <BreakRail {...rail} part="body" inv={inv} setInv={setInv} source={source} /> :
    view === "fix" ? <FixRail {...rail} part="body" setCandidate={setCandidate} /> :
    view === "judge" ? <JudgeRail {...rail} part="body" candidate={candidate} setCandidate={setCandidate} inv={inv} mode={mode} setMode={setMode} differences={differences} setDifferences={setDifferences} /> :
    <EvidenceRail {...rail} part="body" step={step} setStep={setStep} />;
  const canvas =
    view === "break" ? <BreakCanvas doc={doc} inv={inv} setInv={setInv} /> :
    view === "fix" ? <FixCanvas doc={doc} /> :
    view === "judge" ? <JudgeCanvas doc={doc} candidate={candidate} inv={inv} setInv={setInv} mode={mode} differences={differences} /> :
    <EvidenceCanvas doc={doc} step={step} setStep={setStep} />;

  return (
    <main
      className="min-h-dvh pb-16 lg:grid lg:h-dvh lg:grid-cols-[minmax(420px,480px)_1fr] lg:grid-rows-[auto_minmax(0,1fr)_auto] lg:overflow-hidden lg:pb-0 lg:[grid-template-areas:'head_canvas''body_canvas''gate_canvas']"
      data-view={view}
    >
      <div className="px-5 pt-3 lg:px-7 lg:pt-4 lg:[grid-area:head]">
        <nav aria-label="Views" className="-mx-2 flex items-center justify-between gap-1 text-[15px]">
          <Link href="/" className="inline-flex min-h-11 items-center px-2 font-semibold tracking-[-0.01em]" aria-label="Waypoint home">
            Waypoint
          </Link>
          <span className="flex items-center">
            {VIEWS.map((v) => (
              <a
                key={v.id}
                href={`#${v.id}`}
                aria-current={view === v.id ? "page" : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  go(v.id);
                }}
                className={`inline-flex min-h-11 items-center px-2.5 transition-colors ${view === v.id ? "text-ink underline decoration-line-strong decoration-1 underline-offset-[10px]" : "text-faint hover:text-ink"}`}
              >
                {v.label}
              </a>
            ))}
            <Link href="/benchmark" className="inline-flex min-h-11 items-center px-2.5 text-faint transition-colors hover:text-ink max-sm:hidden">
              Benchmark
            </Link>
          </span>
        </nav>
        <div key={view} className="swap">{head}</div>
      </div>
      <div ref={bodyRef} className="px-5 pb-4 lg:min-h-0 lg:overflow-y-auto lg:px-7 lg:pb-10 lg:[grid-area:body] lg:[mask-image:linear-gradient(to_bottom,black_calc(100%-36px),transparent)]">
        <div key={view} className="swap">{body}</div>
      </div>
      <section aria-label="Map" className="px-3 pt-2 lg:min-h-0 lg:p-4 lg:pl-0 lg:[grid-area:canvas]">
        <div className="h-[80dvh] min-h-[640px] lg:h-full lg:min-h-0" key={view}>
          {canvas}
        </div>
      </section>
      <div className="px-5 lg:px-7 lg:[grid-area:gate]">
        <GateLine doc={doc} source={source} />
        <p className="sr-only">
          Run <span data-testid="demo-run-id">{doc.run.run_id}</span>
        </p>
      </div>
    </main>
  );
}

function Skeleton() {
  return (
    <main className="mx-auto w-full max-w-[1280px] px-5 py-20" aria-busy="true" aria-live="polite">
      <div className="h-24 w-24 animate-pulse rounded-lg bg-surface motion-reduce:animate-none" />
      <p className="mt-6 text-sm text-faint">Loading the recorded evidence.</p>
    </main>
  );
}

function EvidenceError({ message }: { message: string }) {
  return (
    <main className="mx-auto max-w-[640px] px-5 py-24">
      <WarningCircle size={28} weight="bold" className="text-bad" aria-hidden />
      <h1 className="mt-4 text-3xl font-semibold tracking-tight">Evidence unavailable</h1>
      <p className="mt-3 text-base leading-[1.6] text-muted">
        The case file expects the committed sanitized evidence at <span className="mono text-ink">public/demo/recorded.json</span>. Run{" "}
        <span className="mono text-ink">make demo</span> for a fresh local replay.
      </p>
      {message ? <p className="mono mt-3 text-sm text-faint">{message}</p> : null}
    </main>
  );
}
