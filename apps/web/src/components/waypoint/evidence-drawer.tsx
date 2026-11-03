"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { X } from "@phosphor-icons/react";

import type { DemoExport, DemoObservation } from "@/lib/demo";
import { callTimeline, cleanEffect, compareInvariants, formatMicros, orderIdentities } from "@/lib/evidence";
import { humanStatus, shortId, sourceLabel, tierNote } from "@/lib/showcase";
import { CopyChip, Fact, Tick, YesNo } from "./kit";
import type { EvidenceSource } from "./types";

const COLS = [
  { key: "before", label: "Original", short: "Orig." },
  { key: "repair", label: "AI repair", short: "Repair" },
  { key: "unsafe", label: "Unsafe seed", short: "Unsafe" },
] as const;

const markColor = (s?: string) => (s === "PASS" ? "text-ok" : s === "FAIL" ? "text-bad" : "text-faint");

function Section({ title, note, children, testId }: { title: string; note?: string; children: React.ReactNode; testId?: string }) {
  return (
    <section className="border-t border-line py-8" data-testid={testId}>
      <h3 className="text-[19px] font-semibold tracking-[-0.02em]">{title}</h3>
      {note ? <p className="mt-2 max-w-[56ch] text-[15px] leading-[1.55] text-muted">{note}</p> : null}
      <div className="mt-5">{children}</div>
    </section>
  );
}

/** Everything behind the board: the rule-by-rule comparison, provider calls, order identities and run facts. */
export function EvidenceDrawer({ doc, source, open, onClose }: { doc: DemoExport; source: EvidenceSource; open: boolean; onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    closeRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const rows = useMemo(() => compareInvariants(doc), [doc]);
  const differing = rows.filter((r) => r.differs);
  const [only, setOnly] = useState(true);
  const [openRow, setOpenRow] = useState<string | null>(differing.find((r) => r.id === "I13")?.id ?? null);
  const shown = only ? differing : rows;
  const ids = orderIdentities(doc);
  const nodes = useMemo(() => callTimeline(doc), [doc]);
  const [call, setCall] = useState(() => Math.max(0, nodes.findIndex((n) => n.obs.capture_id)));
  const obs: DemoObservation | undefined = nodes[call]?.obs;
  const parts = (doc.verification.sandbox ?? "").split(";").map((p) => p.trim()).filter(Boolean);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex justify-end" data-testid="evidence-drawer">
      <button type="button" aria-label="Close the evidence" tabIndex={-1} onClick={onClose} className="absolute inset-0 bg-black/55 backdrop-blur-[2px]" />
      <aside role="dialog" aria-modal="true" aria-label="Evidence" className="enter relative flex h-full w-full max-w-[620px] flex-col bg-background shadow-2xl">
        <header className="flex shrink-0 items-center justify-between border-b border-line px-6 py-4 sm:px-8">
          <div>
            <h2 className="text-[19px] font-semibold tracking-[-0.02em]">Evidence</h2>
            <p className="text-[14px] text-faint">{sourceLabel(doc, source)} · tier {doc.run.evidence_tier}</p>
          </div>
          <button ref={closeRef} type="button" onClick={onClose} aria-label="Close" className="grid size-11 place-items-center rounded-full text-muted transition-colors hover:bg-sunk hover:text-ink">
            <X size={20} aria-hidden />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-16 sm:px-8">
          <Section title="Run" note={tierNote(doc)}>
            <dl>
              <Fact label="Run id"><CopyChip value={doc.run.run_id} label={shortId(doc.run.run_id, 12, 6)} /></Fact>
              <Fact label="Model"><span className="mono break-all">{doc.run.model}</span></Fact>
              <Fact label="Base revision"><span className="mono">{shortId(doc.run.base_revision, 10, 0)}</span></Fact>
              <Fact label="Patch digest"><CopyChip value={doc.patch.digest} label={`${doc.patch.digest.slice(0, 18)}…`} /></Fact>
            </dl>
          </Section>

          <Section title="Rule by rule" note="The checker's verdict on each rule, for all three versions of the code." testId="workpaper">
            <div className="mb-4 flex rounded-full border border-line-strong p-0.5" role="group" aria-label="Row filter">
              {[{ v: true, t: `Differences (${differing.length})` }, { v: false, t: `All ${rows.length}` }].map((o) => (
                <button key={String(o.v)} type="button" aria-pressed={only === o.v} onClick={() => setOnly(o.v)} className="min-h-11 flex-1 rounded-full px-4 text-[15px] font-medium aria-pressed:bg-accent aria-pressed:text-on-accent">
                  {o.t}
                </button>
              ))}
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_repeat(3,3.4rem)] items-end gap-1 border-b border-line py-2 text-[13px] font-medium text-faint sm:grid-cols-[minmax(0,1fr)_repeat(3,4.6rem)]">
              <span>Rule</span>
              {COLS.map((c) => <span key={c.key} className="text-center"><span className="sm:hidden">{c.short}</span><span className="max-sm:hidden">{c.label}</span></span>)}
            </div>
            <ul>
              {shown.map((row) => {
                const isOpen = openRow === row.id;
                return (
                  <li key={row.id} className="border-b border-line">
                    <button
                      type="button"
                      aria-expanded={isOpen}
                      data-row={row.id}
                      onClick={() => setOpenRow(isOpen ? null : row.id)}
                      className="grid w-full grid-cols-[minmax(0,1fr)_repeat(3,3.4rem)] items-center gap-1 py-3 text-left transition-colors hover:bg-surface sm:grid-cols-[minmax(0,1fr)_repeat(3,4.6rem)]"
                    >
                      <span className="flex min-w-0 items-baseline gap-3 pr-1">
                        <span className="w-9 shrink-0 text-[17px] font-semibold">{row.id}</span>
                        <span className="min-w-0 break-words text-[14px] leading-snug text-muted">{(row.repair ?? row.before ?? row.unsafe)?.title ?? row.summary}</span>
                      </span>
                      {COLS.map((c) => (
                        <span key={c.key} className={`flex justify-center ${markColor(row[c.key]?.status)}`} data-mark={`${row.id}-${c.key}`} data-status={row[c.key]?.status}>
                          <Tick status={row[c.key]?.status} size={20} />
                        </span>
                      ))}
                    </button>
                    {isOpen ? (
                      <div className="grid gap-3 pb-5">
                        {COLS.map((c) => {
                          const inv = row[c.key];
                          const cx = inv?.counterexample;
                          return (
                            <div key={c.key} className="rounded-2xl border border-line p-4">
                              <p className="flex items-center justify-between text-[15px] font-semibold">
                                {c.label}
                                <span className={markColor(inv?.status)}>{inv?.status === "PASS" ? "Held" : inv?.status === "FAIL" ? "Failed" : "Not applicable"}</span>
                              </p>
                              {cx ? (
                                <dl className="mt-2">
                                  {cx.order_id ? <Fact label="Order"><span className="mono break-all">{cx.order_id}</span></Fact> : null}
                                  {cx.final_status ? <Fact label="Final status">{humanStatus(cx.final_status)}</Fact> : null}
                                  {cx.confirm_event_id !== undefined ? <Fact label="Journal">event #{cx.confirm_event_id}</Fact> : null}
                                  {cx.effect_count !== undefined ? <Fact label="Effects">{cx.effect_count}</Fact> : null}
                                  {cx.effects?.length ? <Fact label="Effect keys"><span className="mono">{cx.effects.map(cleanEffect).join(", ")}</span></Fact> : null}
                                  {cx.event_id ? <Fact label="Backing event"><span className="mono">{cx.event_id}</span></Fact> : null}
                                </dl>
                              ) : (
                                <p className="mt-2 text-[14px] leading-[1.5] text-muted">{inv?.status === "PASS" ? "No counterexample: the rule held." : inv?.status === "FAIL" ? "No counterexample fields exported." : "This rule does not apply to the scenario that ran."}</p>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </Section>

          <Section
            title="Provider calls"
            note={`These ${nodes.length} calls belong to the verification order${ids.verification ? ` ${shortId(ids.verification, 8, 4)}` : ""}, not the incident order. The gaps between them are the recorded ones.`}
            testId="provider-calls"
          >
            <ol className="flex flex-wrap gap-2" aria-label="Provider calls">
              {nodes.map((n, i) => (
                <li key={n.obs.seq}>
                  <button
                    type="button"
                    data-testid="observation-row"
                    aria-pressed={call === i}
                    onClick={() => setCall(i)}
                    className="min-h-11 rounded-full border border-line-strong px-4 text-left text-[14px] transition-colors aria-pressed:border-accent aria-pressed:bg-accent aria-pressed:text-on-accent"
                  >
                    <span className="font-semibold">{n.obs.seq}</span> <span className="mono">{n.obs.method}</span>
                    <span className="ml-1.5 opacity-70">{i === 0 ? "start" : `+${formatMicros(n.gapUs)}`}</span>
                  </button>
                </li>
              ))}
            </ol>
            {obs ? (
              <dl className="mt-5" aria-live="polite" data-testid="call-detail">
                <Fact label="Order status">{humanStatus(obs.order_status)}</Fact>
                <Fact label="Provider ref"><span className="mono">{obs.order_ref ?? "not recorded"}</span></Fact>
                <Fact label="Request id">{obs.request_id ? <CopyChip value={obs.request_id} label={shortId(obs.request_id, 8, 4)} /> : "not recorded"}</Fact>
                <Fact label="Durable first"><YesNo value={obs.request_id_durable_before_call} /></Fact>
                <Fact label="Sent amount">{obs.sent_amount ? `${obs.sent_amount} ${obs.sent_currency ?? ""}` : "not recorded"}</Fact>
                <Fact label="Mutation applied"><YesNo value={obs.mutation_applied} /></Fact>
                <Fact label="Idempotent replay"><YesNo value={obs.idempotent_replay} /></Fact>
                <Fact label="Response dropped"><YesNo value={obs.response_dropped} /></Fact>
                {obs.capture_id ? <Fact label="Capture"><span className="mono">{obs.capture_id} · {obs.capture_status} · {obs.capture_amount} {obs.capture_currency}</span></Fact> : null}
                <Fact label="Source"><span className="mono">{obs.source ?? "not recorded"} · tier {obs.tier ?? "?"}</span></Fact>
              </dl>
            ) : null}
          </Section>

          <Section title="Three orders, kept apart" note="The incident, the verification run and the unsafe run each used a different order, so one never explains another.">
            <div className="flex flex-col gap-3 text-[15px] text-faint">
              <span className="flex items-center justify-between gap-3">Incident <CopyChip value={ids.incident} label={shortId(ids.incident, 8, 4)} /></span>
              {ids.verification ? <span className="flex items-center justify-between gap-3">Verification <CopyChip value={ids.verification} label={shortId(ids.verification, 8, 4)} /></span> : null}
              {ids.unsafeRun ? <span className="flex items-center justify-between gap-3">Unsafe run <CopyChip value={ids.unsafeRun} label={shortId(ids.unsafeRun, 8, 4)} /></span> : null}
            </div>
          </Section>

          {parts.length ? (
            <Section title="Isolation" note="What the candidate ran under in the sandbox.">
              <ul>{parts.map((p) => <li key={p} className="mono py-0.5 text-[14px] text-muted">{p}</li>)}</ul>
            </Section>
          ) : null}
        </div>
      </aside>
    </div>
  );
}
