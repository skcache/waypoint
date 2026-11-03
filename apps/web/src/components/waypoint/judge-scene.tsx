"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import type { DemoExport } from "@/lib/demo";
import { cleanEffect, diffNotes, receiptModel, type Column } from "@/lib/evidence";
import { humanStatus, money, titleFor } from "@/lib/showcase";
import { CodeViewer } from "./code-viewer";
import { Reveal } from "./kit";
import { Receipt, ROW_INVARIANTS, type RowKey } from "./receipt";

const COLUMNS: Column[] = ["before", "repair", "unsafe"];
const ease = (t: number) => (t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2);

export function JudgeScene({ doc }: { doc: DemoExport }) {
  const models = useMemo(() => Object.fromEntries(COLUMNS.map((c) => [c, receiptModel(doc, c)])) as Record<Column, ReturnType<typeof receiptModel>>, [doc]);
  const amount = money(doc.incident.total_minor, doc.incident.currency);
  const caseLabel = `Case file ${doc.incident.fault} · ${titleFor(doc)}`;
  const [lens, setLens] = useState(0);
  const [picked, setPicked] = useState<{ col: Column; row: RowKey } | null>(null);
  const [code, setCode] = useState(false);
  const raf = useRef(0);
  const lensRef = useRef(0);

  useEffect(() => {
    lensRef.current = lens;
  }, [lens]);
  useEffect(() => () => cancelAnimationFrame(raf.current), []);

  function sweep(to: number) {
    cancelAnimationFrame(raf.current);
    const from = lensRef.current;
    const start = performance.now();
    const ms = 1700 * Math.max(0.35, Math.abs(to - from));
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / ms);
      setLens(from + (to - from) * ease(t));
      if (t < 1) raf.current = requestAnimationFrame(tick);
    };
    raf.current = requestAnimationFrame(tick);
  }

  const stamped = lens > 0.93;
  const unsafeFails = models.unsafe.failed;
  const cleared = models.before.failed.filter((id) => !unsafeFails.includes(id));
  const surfaceSame = models.repair.status === models.unsafe.status;

  const caption =
    lens < 0.12
      ? surfaceSame
        ? "At a glance the AI repair and the unsafe patch are the same: the same status, a completed capture, fulfilled."
        : "At a glance, the candidates differ only in their order status."
      : stamped
        ? `The judge reads the ledger. The AI repair fulfills ${models.repair.fulfilled ?? "?"} time; the unsafe patch still fulfills ${models.unsafe.fulfilled ?? "?"}.`
        : "Keep going. The ledger is underneath.";

  const outcome: Record<Column, string> = {
    before: `Fails ${models.before.failed.join(", ")}. The status rolled back and the payment fulfilled twice.`,
    repair: `${models.repair.ticks.filter((t) => t.status === "PASS").length} of ${models.repair.ticks.filter((t) => t.status !== "NOT_APPLICABLE").length} applicable invariants pass. One capture, one fulfillment.`,
    unsafe: `Fails ${unsafeFails.join(", ")}.${cleared.length ? ` It clears ${cleared.join(" and ")}: the visible symptom is gone, the money problem is not.` : ""}`,
  };

  const pickedInv =
    picked &&
    (picked.col === "before" ? doc.pre_repair : picked.col === "repair" ? doc.oracle : doc.unsafe).invariants.filter((i) => ROW_INVARIANTS[picked.row].includes(i.id));

  return (
    <section id="judge" data-scene="judge">
      <div className="field-paper">
        <div className="mx-auto w-full max-w-[1440px] px-5 pb-24 pt-24 sm:px-8 lg:px-12 lg:pb-32 lg:pt-36">
          <Reveal>
            <h2 className="poster text-[clamp(3.8rem,11vw,10.5rem)]">
              Looks fixed.
              <br />
              <span className="marker marker-alarm px-[0.1em]" data-on={lens > 0.5 ? "true" : "false"}>Is it?</span>
            </h2>
            <p className="mt-8 max-w-[46ch] text-[clamp(1.15rem,1.8vw,1.45rem)] leading-[1.4]">
              A separate program re-reads the merchant&rsquo;s ledger after each candidate runs and scores {doc.oracle.invariants.length} invariants. It does not trust the
              candidate, and it does not glance. Drag the lens down through all three receipts.
            </p>
          </Reveal>

          <div className="sticky top-14 z-30 -mx-5 mt-10 border-y-2 border-night bg-background px-5 py-3 sm:-mx-8 sm:px-8 lg:mx-0 lg:border-2 lg:border-night lg:px-5" data-testid="lens-bar">
            <div className="grid grid-cols-[1fr_auto] items-center gap-x-4 gap-y-1 lg:flex lg:flex-wrap lg:gap-x-6">
              <label htmlFor="lens" className="poster text-[28px]">The judge&rsquo;s lens</label>
              <div className="order-last col-span-2 flex min-w-0 flex-1 items-center gap-3 lg:order-none lg:min-w-[240px]">
                <span className={`text-[15px] font-semibold ${lens < 0.5 ? "" : "opacity-45"}`}>Surface</span>
                <input
                  id="lens"
                  data-testid="lens"
                  type="range"
                  min={0}
                  max={1000}
                  value={Math.round(lens * 1000)}
                  onChange={(e) => {
                    cancelAnimationFrame(raf.current);
                    setLens(Number(e.target.value) / 1000);
                  }}
                  aria-valuetext={lens < 0.12 ? "Surface" : stamped ? "Ledger" : `${Math.round(lens * 100)} percent down`}
                  className="h-11 w-full min-w-0 cursor-ew-resize appearance-none bg-transparent [&::-moz-range-thumb]:h-9 [&::-moz-range-thumb]:w-5 [&::-moz-range-thumb]:rounded-none [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-alarm [&::-moz-range-track]:h-1 [&::-moz-range-track]:bg-night [&::-webkit-slider-runnable-track]:h-1 [&::-webkit-slider-runnable-track]:bg-night [&::-webkit-slider-thumb]:-mt-4 [&::-webkit-slider-thumb]:h-9 [&::-webkit-slider-thumb]:w-5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-none [&::-webkit-slider-thumb]:border-2 [&::-webkit-slider-thumb]:border-night [&::-webkit-slider-thumb]:bg-alarm"
                />
                <span className={`text-[15px] font-semibold ${lens >= 0.5 ? "" : "opacity-45"}`}>Ledger</span>
              </div>
              <button
                type="button"
                onClick={() => sweep(lens > 0.5 ? 0 : 1)}
                className="inline-flex h-11 items-center bg-night px-4 text-[15px] font-semibold text-paper transition-transform hover:-translate-y-0.5 lg:h-12 lg:px-5 lg:text-[16px]"
              >
                {lens > 0.5 ? "Back to the surface" : "Look under the surface"}
              </button>
            </div>
          </div>
          <p className="mt-5 min-h-[3.2rem] max-w-[60ch] text-[clamp(1.1rem,1.6vw,1.35rem)] font-medium leading-[1.4]" aria-live="polite" data-testid="lens-caption">
            {caption}
          </p>

          <div className="mt-8 grid justify-items-center gap-14 lg:grid-cols-3 lg:items-start lg:gap-8 xl:gap-14">
            {COLUMNS.map((col, i) => (
              <div key={col} className="w-full min-w-0 max-w-[400px]">
                <Reveal kind="print" delay={i * 140}>
                  <Receipt
                    model={models[col]}
                    amount={amount}
                    caseLabel={caseLabel}
                    lens={lens}
                    stamped={stamped}
                    selected={picked?.col === col ? picked.row : null}
                    onRow={lens > 0.5 ? (row) => setPicked((p) => (p?.col === col && p.row === row ? null : { col, row })) : undefined}
                    testId={`receipt-${col}`}
                  />
                </Reveal>
                <div className="mt-6 min-h-[8.5rem] text-[16px] leading-[1.5]" data-testid={`outcome-${col}`}>
                  <p className={`font-semibold transition-opacity duration-500 ${stamped ? "opacity-100" : "opacity-0"}`}>{outcome[col]}</p>
                  <p className="mt-3 text-[15px] leading-[1.55] text-muted" data-testid={`provenance-${col}`}>
                    {col === "before" ? (
                      "Recorded pre-repair evaluation of the incident order."
                    ) : col === "repair" ? (
                      <>
                        Verified for the tested scenario on a tier {doc.run.evidence_tier} scripted fixture. This is not a claim about live PayPal behavior or scenarios that were not run.
                      </>
                    ) : (
                      <>
                        Historical independent-oracle evidence. {doc.unsafe.provenance}{" "}
                        {doc.unsafe.fresh_trusted_gate.executed
                          ? `Fresh trusted gate: ${humanStatus(doc.unsafe.fresh_trusted_gate.result)}. ${doc.unsafe.fresh_trusted_gate.detail ?? ""}`
                          : "The fresh trusted gate was not run for this recorded artifact."}
                      </>
                    )}
                  </p>
                </div>
              </div>
            ))}
          </div>

          <div className="mt-6 min-h-[5.5rem] max-w-[70ch] border-t-2 border-night pt-4" aria-live="polite" data-testid="judge-note">
            {pickedInv && pickedInv.length ? (
              pickedInv.map((inv) => (
                <p key={inv.id} className="text-[17px] leading-snug">
                  <b className="font-bold">{models[picked!.col].label} · {inv.id} {inv.status === "FAIL" ? "fails" : "holds"}.</b> {inv.title ?? inv.summary}
                  {inv.counterexample?.effects ? <span className="mono"> ({inv.counterexample.effects.map(cleanEffect).join(", ")})</span> : null}
                </p>
              ))
            ) : (
              <p className="text-[17px] leading-snug text-muted">{lens > 0.5 ? "Select a line on any receipt to see which invariant it is evidence for." : "Drag the lens to read the ledgers."}</p>
            )}
          </div>

          <div className="mt-12">
            <button
              type="button"
              aria-expanded={code}
              onClick={() => setCode((v) => !v)}
              className="inline-flex h-14 items-center bg-night px-6 text-[17px] font-semibold text-paper transition-transform hover:-translate-y-0.5"
            >
              {code ? "Hide the patches" : "Read the two patches"}
            </button>
          </div>
        </div>
      </div>

      {code ? (
        <div className="field-ink" data-testid="patch-band">
          <div className="mx-auto grid w-full max-w-[1440px] gap-8 px-5 py-16 sm:px-8 lg:px-12 xl:grid-cols-2">
            <CodeViewer
              testId="judge-diff-repair"
              heading="AI repair"
              file={doc.patch.changed_files[0] ?? "patch"}
              diff={doc.patch.diff}
              digest={doc.patch.digest}
              notes={diffNotes(doc.patch.digest)}
              maxHeight="640px"
            />
            {doc.unsafe.diff ? (
              <CodeViewer
                testId="demo-unsafe-diff"
                heading="Seeded unsafe patch"
                file={doc.unsafe.changed_files?.[0] ?? "patch"}
                diff={doc.unsafe.diff}
                digest={doc.unsafe.diff_digest}
                notes={diffNotes(doc.unsafe.diff_digest)}
                maxHeight="640px"
              />
            ) : (
              <p className="self-center text-muted">This recorded evaluation exported no patch for the unsafe candidate.</p>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
}
