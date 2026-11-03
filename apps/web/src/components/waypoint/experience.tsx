"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { WarningCircle } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { BreakScene } from "./break-scene";
import { FixScene } from "./fix-scene";
import { InkDefs } from "./kit";
import { JudgeScene } from "./judge-scene";
import { ProofScene } from "./proof-scene";
import { SignatureBar } from "./signature-bar";
import type { EvidenceSource } from "./types";

type Variant = "showcase" | "demo";

const SCENES = [
  { id: "break", label: "Break", field: "field-alarm" },
  { id: "fix", label: "Fix", field: "field-ink" },
  { id: "judge", label: "Judge", field: "field-paper" },
  { id: "proof", label: "Proof", field: "field-ink" },
] as const;

function Header() {
  const [field, setField] = useState<string>("field-alarm");
  const [active, setActive] = useState<string>("break");
  useEffect(() => {
    const els = SCENES.map((s) => document.getElementById(s.id)).filter(Boolean) as HTMLElement[];
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const scene = SCENES.find((s) => s.id === e.target.id);
          if (scene) {
            setField(scene.field);
            setActive(scene.id);
          }
        }
      },
      { rootMargin: "0px 0px -92% 0px" },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);
  return (
    <header className={`${field} sticky top-0 z-40 transition-colors duration-300`} data-field={field}>
      <div className="mx-auto flex h-14 w-full max-w-[1440px] items-center justify-between px-3 sm:px-8 lg:px-12">
        <a href="#break" className="poster inline-flex min-h-11 items-center px-2 text-[26px]" aria-label="Waypoint, back to the top">
          Waypoint
        </a>
        <nav aria-label="Sections" className="flex items-center">
          {SCENES.map((s) => (
            <a
              key={s.id}
              href={`#${s.id}`}
              aria-current={active === s.id ? "location" : undefined}
              className={`inline-flex min-h-11 items-center px-2.5 text-[15px] font-semibold sm:px-3.5 ${active === s.id ? "underline decoration-2 underline-offset-[7px]" : "opacity-70 hover:opacity-100"}`}
            >
              {s.label}
            </a>
          ))}
          <Link href="/benchmark" className="inline-flex min-h-11 items-center px-3.5 text-[15px] font-semibold opacity-70 hover:opacity-100 max-sm:hidden">
            Benchmark
          </Link>
        </nav>
      </div>
    </header>
  );
}

export function Showcase({ initial, variant }: { initial: DemoExport | null; variant: Variant }) {
  const [doc, setDoc] = useState<DemoExport | null>(initial);
  const [source, setSource] = useState<EvidenceSource>("recorded");
  const [loading, setLoading] = useState(!initial);
  const [error, setError] = useState("");

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

  return (
    <main className="pb-14">
      <InkDefs />
      <Header />
      <BreakScene doc={doc} source={source} />
      <FixScene doc={doc} />
      <JudgeScene doc={doc} />
      <ProofScene doc={doc} />
      <p className="sr-only">
        Run <span data-testid="demo-run-id">{doc.run.run_id}</span>
      </p>
      <SignatureBar doc={doc} source={source} />
    </main>
  );
}

function Skeleton() {
  return (
    <main className="field-alarm min-h-dvh px-5 py-20" aria-busy="true" aria-live="polite">
      <div className="poster text-[clamp(4rem,12vw,10rem)]">Waypoint</div>
      <p className="mt-6 text-[17px]">Loading the recorded evidence.</p>
    </main>
  );
}

function EvidenceError({ message }: { message: string }) {
  return (
    <main className="mx-auto max-w-[640px] px-5 py-24">
      <WarningCircle size={28} weight="bold" className="text-bad" aria-hidden />
      <h1 className="poster mt-4 text-5xl">Evidence unavailable</h1>
      <p className="mt-3 text-base leading-[1.6] text-muted">
        The case file expects the committed sanitized evidence at <span className="mono text-ink">public/demo/recorded.json</span>. Run{" "}
        <span className="mono text-ink">make demo</span> for a fresh local replay.
      </p>
      {message ? <p className="mono mt-3 text-sm text-faint">{message}</p> : null}
    </main>
  );
}
