"use client";

import type { DemoEval, DemoExport } from "@/lib/demo";
import { effectCount, failedIds, verdictWord } from "@/lib/evidence";
import { money, sourceLabel, titleFor } from "@/lib/showcase";
import { btnPrimary, btnQuiet, Container, VerdictTag } from "./ui";
import type { Candidate, EvidenceSource } from "./types";

type Row = { key: Candidate; label: string; e: DemoEval };

export function Hero({
  doc,
  source,
  candidate,
  onPick,
}: {
  doc: DemoExport;
  source: EvidenceSource;
  candidate: Candidate;
  onPick: (c: Candidate) => void;
}) {
  const rows: Row[] = [
    { key: "before", label: "The original code", e: doc.pre_repair },
    { key: "repair", label: "The AI's patch", e: doc.oracle },
    { key: "unsafe", label: "A seeded unsafe patch", e: doc.unsafe },
  ];
  const amount = money(doc.incident.total_minor, doc.incident.currency);

  return (
    <section id="top" className="border-b border-line">
      <Container>
        <div className="grid gap-14 py-16 sm:py-24 lg:grid-cols-[1.15fr_.85fr] lg:items-end lg:gap-20 lg:py-32">
          <div>
            <h1 className="text-balance text-[clamp(2.6rem,7vw,5rem)] font-semibold leading-[1] tracking-[-0.035em]">
              An AI fixed a payment bug.
            </h1>
            <p className="mt-6 max-w-[30ch] text-pretty text-[clamp(1.25rem,2.2vw,1.75rem)] leading-[1.3] tracking-[-0.01em]">
              Then a separate judge checked the ledger, and rejected a patch that only looked fixed.
            </p>
            <p className="mt-6 max-w-[52ch] text-base leading-[1.6] text-muted">
              Case {doc.incident.fault}, {titleFor(doc).toLowerCase()}: one {amount} payment, fulfilled twice. This is {sourceLabel(doc, source)} from a
              scripted provider fixture (tier {doc.run.evidence_tier}); no live model, PayPal call or payment runs on
              this page.
            </p>
            <div className="mt-9 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-4">
              <a href="#break" className={btnPrimary}>See what broke</a>
              <a href="#judge" className={btnQuiet}>Jump to the verdict</a>
            </div>
          </div>

          <div role="radiogroup" aria-label="Open a candidate in the judge" className="rounded-panel bg-surface">
            <p className="label px-5 pt-4">One payment, three candidates, one judge</p>
            <ul className="mt-2 pb-2">
              {rows.map((row) => {
                const count = effectCount(row.e);
                const failed = failedIds(row.e);
                const on = candidate === row.key;
                return (
                  <li key={row.key}>
                    <button
                      type="button"
                      role="radio"
                      aria-checked={on}
                      onClick={() => onPick(row.key)}
                      className={`grid min-h-[84px] w-full grid-cols-[1fr_auto] items-center gap-4 px-5 py-4 text-left transition-colors hover:bg-sunk ${on ? "bg-sunk" : ""}`}
                    >
                      <span className="min-w-0">
                        <span className="block text-lg font-medium leading-snug">{row.label}</span>
                        <span className="mt-0.5 block text-[15px] leading-snug text-muted">
                          {count === null ? "fulfillments not stated" : `${count} fulfillment${count === 1 ? "" : "s"}`}
                          {failed.length ? ` · fails ${failed.join(", ")}` : " · no invariant fails"}
                        </span>
                      </span>
                      <VerdictTag word={verdictWord(row.e.verdict)} />
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        </div>
      </Container>
    </section>
  );
}
