"use client";

import { useEffect, useState } from "react";
import { WarningCircle } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { BreakAct } from "./break-act";
import { FixAct } from "./fix-act";
import { Hero } from "./hero";
import { HumanGate } from "./human-gate";
import { JudgeAct } from "./judge-act";
import { Container } from "./ui";
import type { Candidate, EvidenceSource } from "./types";

type Variant = "showcase" | "demo";

export function Showcase({ initial, variant }: { initial: DemoExport | null; variant: Variant }) {
  const [doc, setDoc] = useState<DemoExport | null>(initial);
  const [source, setSource] = useState<EvidenceSource>("recorded");
  const [loading, setLoading] = useState(!initial);
  const [error, setError] = useState("");
  const [candidate, setCandidate] = useState<Candidate>("repair");

  useEffect(() => {
    let live = true;
    async function load() {
      if (initial) {
        setDoc(initial);
        setLoading(false);
      }
      const forceRecorded =
        typeof window !== "undefined" && new URLSearchParams(window.location.search).get("source") === "recorded";
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

  function pick(next: Candidate) {
    setCandidate(next);
    document.getElementById("judge")?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  return (
    <main className="pb-14">
      <Hero doc={doc} source={source} candidate={candidate} onPick={pick} />
      <BreakAct doc={doc} />
      <FixAct doc={doc} onChallenge={setCandidate} />
      <JudgeAct doc={doc} candidate={candidate} setCandidate={setCandidate} />
      <footer className="night border-t border-line">
        <Container className="flex flex-col gap-2 py-8 text-[15px] text-faint sm:flex-row sm:items-center sm:justify-between">
          <p>
            Run <span className="mono break-all text-muted" data-testid="demo-run-id">{doc.run.run_id}</span>
          </p>
          <p>Case file · recorded evidence · no live model, PayPal call or payment.</p>
        </Container>
      </footer>
      <HumanGate doc={doc} source={source} />
    </main>
  );
}

function Skeleton() {
  return (
    <main className="mx-auto w-full max-w-[1280px] px-5 py-20 sm:px-8 lg:px-12" aria-busy="true" aria-live="polite">
      <div className="h-4 w-48 animate-pulse rounded bg-sunk motion-reduce:animate-none" />
      <div className="mt-6 h-24 w-4/5 animate-pulse rounded bg-sunk motion-reduce:animate-none" />
      <div className="mt-4 h-6 w-3/5 animate-pulse rounded bg-sunk motion-reduce:animate-none" />
      <p className="mt-8 text-sm text-faint">Loading the recorded evidence.</p>
    </main>
  );
}

function EvidenceError({ message }: { message: string }) {
  return (
    <main className="mx-auto max-w-[640px] px-5 py-24">
      <WarningCircle size={28} weight="bold" className="text-bad" aria-hidden />
      <h1 className="mt-4 text-3xl font-semibold tracking-tight">Evidence unavailable</h1>
      <p className="mt-3 text-base leading-[1.6] text-muted">
        The case file expects the committed sanitized evidence at <span className="mono text-ink">public/demo/recorded.json</span>.
        Run <span className="mono text-ink">make demo</span> for a fresh local replay.
      </p>
      {message ? <p className="mono mt-3 text-sm text-faint">{message}</p> : null}
    </main>
  );
}
