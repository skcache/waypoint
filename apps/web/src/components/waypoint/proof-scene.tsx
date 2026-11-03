"use client";

import { useMemo, useState } from "react";

import type { DemoExport, DemoObservation } from "@/lib/demo";
import { callTimeline, cleanEffect, compareInvariants, formatMicros, formatMs, orderIdentities, totalStepMs } from "@/lib/evidence";
import { humanStatus, shortId } from "@/lib/showcase";
import { CopyChip, Fact, Reveal, Tick, YesNo } from "./kit";

const COLS = [
  { key: "before", label: "Original", short: "Before" },
  { key: "repair", label: "AI repair", short: "Repair" },
  { key: "unsafe", label: "Unsafe seed", short: "Unsafe" },
] as const;

const markColor = (s?: string) => (s === "PASS" ? "text-ok" : s === "FAIL" ? "text-bad" : "text-faint");

export function ProofScene({ doc }: { doc: DemoExport }) {
  const rows = useMemo(() => compareInvariants(doc), [doc]);
  const differing = rows.filter((r) => r.differs);
  const [only, setOnly] = useState(true);
  const [open, setOpen] = useState<string | null>(differing.find((r) => r.id === "I13")?.id ?? null);
  const shown = only ? differing : rows;
  const ids = orderIdentities(doc);

  const steps = doc.verification.steps;
  const total = totalStepMs(steps);
  const [step, setStep] = useState(() => [...steps].sort((a, b) => b.duration_ms - a.duration_ms)[0]?.name ?? "");
  const parts = (doc.verification.sandbox ?? "").split(";").map((p) => p.trim()).filter(Boolean);

  const nodes = useMemo(() => callTimeline(doc), [doc]);
  const [call, setCall] = useState(() => Math.max(0, nodes.findIndex((n) => n.obs.capture_id)));
  const obs: DemoObservation | undefined = nodes[call]?.obs;

  return (
    <section id="proof" className="field-ink" data-scene="proof">
      <div className="mx-auto w-full max-w-[1440px] px-5 pb-40 pt-24 sm:px-8 lg:px-12 lg:pb-48 lg:pt-36">
        <Reveal>
          <h2 className="poster text-[clamp(2.8rem,6.6vw,6.2rem)]">The proof.</h2>
          <p className="mt-8 max-w-[44ch] text-[clamp(1.15rem,1.8vw,1.45rem)] leading-[1.4] text-muted">
            Everything behind the receipts: the {rows.length} invariants side by side, the sandbox the repair ran in, and the provider calls it made.
          </p>
        </Reveal>

        {/* Workpaper */}
        <div className="mt-20" data-testid="workpaper">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <h3 className="poster text-[clamp(2rem,4vw,3.4rem)]">Workpaper</h3>
            <div className="flex rounded-full border border-line-strong p-0.5" role="group" aria-label="Row filter">
              {[{ v: true, t: `Differences (${differing.length})` }, { v: false, t: `All ${rows.length}` }].map((o) => (
                <button key={String(o.v)} type="button" aria-pressed={only === o.v} onClick={() => setOnly(o.v)} className="min-h-11 px-4 text-[15px] font-semibold aria-pressed:bg-accent aria-pressed:text-on-accent aria-pressed:rounded-full">
                  {o.t}
                </button>
              ))}
            </div>
          </div>
          <div className="mt-6 border-t border-line-strong">
            <div className="grid grid-cols-[minmax(0,1fr)_repeat(3,3rem)] items-end gap-1 border-b border-line py-3 text-[13px] font-semibold text-faint sm:grid-cols-[1fr_repeat(3,7rem)] sm:gap-2 sm:text-[15px]">
              <span>Invariant</span>
              {COLS.map((c) => <span key={c.key} className="text-center"><span className="sm:hidden">{c.short}</span><span className="max-sm:hidden">{c.label}</span></span>)}
            </div>
            <ul>
              {shown.map((row) => {
                const isOpen = open === row.id;
                return (
                  <li key={row.id} className="border-b border-line">
                    <button
                      type="button"
                      aria-expanded={isOpen}
                      data-row={row.id}
                      onClick={() => setOpen(isOpen ? null : row.id)}
                      className="grid w-full grid-cols-[minmax(0,1fr)_repeat(3,3rem)] items-center gap-1 py-4 text-left transition-colors hover:bg-surface sm:grid-cols-[1fr_repeat(3,7rem)] sm:gap-2"
                    >
                      <span className="flex min-w-0 items-baseline gap-2 pr-1 sm:gap-4 sm:pr-2">
                        <span className="poster w-10 shrink-0 text-[22px] sm:w-14 sm:text-[26px]">{row.id}</span>
                        <span className="min-w-0 break-words text-[15px] leading-snug sm:text-[16px]">{(row.repair ?? row.before ?? row.unsafe)?.title ?? row.summary}</span>
                      </span>
                      {COLS.map((c) => (
                        <span key={c.key} className={`flex justify-center ${markColor(row[c.key]?.status)}`} data-mark={`${row.id}-${c.key}`} data-status={row[c.key]?.status}>
                          <Tick status={row[c.key]?.status} size={22} />
                        </span>
                      ))}
                    </button>
                    {isOpen ? (
                      <div className="grid gap-4 pb-6 md:grid-cols-3">
                        {COLS.map((c) => {
                          const inv = row[c.key];
                          const cx = inv?.counterexample;
                          return (
                            <div key={c.key} className="border border-line p-4">
                              <p className="flex items-center justify-between text-[15px] font-semibold">
                                {c.label}
                                <span className={markColor(inv?.status)}>{inv?.status === "PASS" ? "Pass" : inv?.status === "FAIL" ? "Fail" : "N/A"}</span>
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
                                <p className="mt-2 text-[15px] leading-[1.5] text-muted">{inv?.status === "PASS" ? "No counterexample: the invariant held." : inv?.status === "FAIL" ? "No counterexample fields exported." : "Not applicable to this scenario."}</p>
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
          </div>
          <p className="mt-3 text-[15px] text-faint">N/A means the invariant does not apply to the scenario that ran.</p>
        </div>

        {/* Sandbox */}
        <div className="mt-28" data-testid="sandbox">
          <div className="flex flex-wrap items-end gap-x-8 gap-y-2">
            <h3 className="poster text-[clamp(2rem,4vw,3.4rem)]">Sandbox</h3>
            <p className="poster text-[clamp(3rem,7vw,6rem)] text-clear">{(total / 1000).toFixed(1)} s</p>
            <p className="max-w-[34ch] pb-2 text-[17px] leading-snug text-muted">in a disposable Docker sandbox. {steps.every((s) => s.passed) ? "All checks passed." : "A check failed."}</p>
          </div>
          <div className="mt-8 flex h-24 gap-1" role="group" aria-label="Sandbox steps, widths are recorded durations">
            {steps.map((s) => (
              <button
                key={s.name}
                type="button"
                data-step={s.name}
                aria-pressed={step === s.name}
                aria-label={`${s.name}: ${formatMs(s.duration_ms)}`}
                onClick={() => setStep(s.name)}
                style={{ flexGrow: s.duration_ms, flexBasis: 0, background: step === s.name ? "var(--clear)" : s.passed ? "var(--c-text)" : "var(--alarm)" }}
                className="min-w-[6px] transition-colors hover:opacity-85"
              />
            ))}
          </div>
          <ul className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-5" data-testid="gate-steps">
            {steps.map((s) => (
              <li key={s.name} className={step === s.name ? "" : "opacity-70"}>
                <button type="button" onClick={() => setStep(s.name)} className="text-left">
                  <span className="mono block text-[17px] font-bold">{s.name}</span>
                  <span className="block text-[15px] tabular-nums text-muted">
                    {formatMs(s.duration_ms)} · {Math.round((s.duration_ms / total) * 100)}% · {s.passed ? "passed" : "failed"}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <p className="mt-4 max-w-[70ch] text-[15px] leading-[1.55] text-faint">
            Widths are the real recorded durations. Command output stays in the local artifact directory and is not part of the public export.
          </p>
          {parts.length ? (
            <details className="group mt-5 max-w-[760px] border-y border-line">
              <summary className="flex min-h-14 items-center justify-between text-[17px] font-semibold">
                Isolation the candidate ran under
                <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
              </summary>
              <ul className="grid gap-x-8 pb-4 sm:grid-cols-2">{parts.map((p) => <li key={p} className="mono py-0.5 text-[15px] text-muted">{p}</li>)}</ul>
            </details>
          ) : null}
        </div>

        {/* Provider calls */}
        <div className="mt-28" data-testid="provider-calls">
          <h3 className="poster text-[clamp(2rem,4vw,3.4rem)]">Provider calls</h3>
          <p className="mt-4 max-w-[62ch] text-[17px] leading-[1.5] text-muted">
            These {nodes.length} calls belong to the verification order{ids.verification ? ` ${shortId(ids.verification, 8, 4)}` : ""}, not the incident order. Real gaps between them are shown.
          </p>
          <ol className="mt-8 grid" style={{ gridTemplateColumns: `repeat(${nodes.length}, minmax(0, 1fr))` }} aria-label="Provider calls">
            {nodes.map((n, i) => (
              <li key={n.obs.seq} className="relative flex flex-col items-start border-t-2 border-line-strong pt-4" style={{ borderColor: i <= call ? "var(--c-text)" : undefined }}>
                <button type="button" data-testid="observation-row" aria-pressed={call === i} onClick={() => setCall(i)} className="text-left">
                  <span className={`poster block text-[clamp(2rem,4vw,3.4rem)] ${call === i ? "text-clear" : ""}`}>{n.obs.seq}</span>
                  <span className="mono block text-[17px] font-bold">{n.obs.method}</span>
                  <span className="block text-[15px] text-faint">{i === 0 ? "start" : `+${formatMicros(n.gapUs)}`}</span>
                </button>
              </li>
            ))}
          </ol>
          {obs ? (
            <dl className="mt-8 max-w-[760px] border-t border-line" aria-live="polite" data-testid="call-detail">
              <Fact label="Order status">{humanStatus(obs.order_status)}</Fact>
              <Fact label="Provider order ref"><span className="mono">{obs.order_ref ?? "not recorded"}</span></Fact>
              <Fact label="Request id">{obs.request_id ? <CopyChip value={obs.request_id} label={shortId(obs.request_id, 8, 4)} /> : "not recorded"}</Fact>
              <Fact label="Durable before call"><YesNo value={obs.request_id_durable_before_call} /></Fact>
              <Fact label="Sent amount">{obs.sent_amount ? `${obs.sent_amount} ${obs.sent_currency ?? ""}` : "not recorded"}</Fact>
              <Fact label="Mutation applied"><YesNo value={obs.mutation_applied} /></Fact>
              <Fact label="Idempotent replay"><YesNo value={obs.idempotent_replay} /></Fact>
              <Fact label="Response dropped"><YesNo value={obs.response_dropped} /></Fact>
              {obs.capture_id ? <Fact label="Capture"><span className="mono">{obs.capture_id} · {obs.capture_status} · {obs.capture_amount} {obs.capture_currency}</span></Fact> : null}
              <Fact label="Source"><span className="mono">{obs.source ?? "not recorded"} · tier {obs.tier ?? "?"}</span></Fact>
            </dl>
          ) : null}
        </div>

        {/* Orders and next */}
        <div className="mt-28 grid gap-10 border-t border-line-strong pt-10 lg:grid-cols-[1fr_auto] lg:items-end">
          <div>
            <h3 className="poster text-[clamp(2rem,4vw,3.4rem)]">Three orders, kept apart</h3>
            <div className="mt-5 flex flex-wrap gap-x-8 gap-y-3 text-[15px] text-faint">
              <span className="inline-flex items-center gap-2">Incident <CopyChip value={ids.incident} label={shortId(ids.incident, 8, 4)} /></span>
              {ids.verification ? <span className="inline-flex items-center gap-2">Verification <CopyChip value={ids.verification} label={shortId(ids.verification, 8, 4)} /></span> : null}
              {ids.unsafeRun ? <span className="inline-flex items-center gap-2">Unsafe run <CopyChip value={ids.unsafeRun} label={shortId(ids.unsafeRun, 8, 4)} /></span> : null}
            </div>
          </div>
          <a href="/benchmark" className="inline-flex h-12 items-center rounded-full bg-accent px-6 text-[16px] font-medium text-on-accent transition-transform hover:-translate-y-0.5">
            One case is a demonstration. See the held-out benchmark (preliminary)
          </a>
        </div>
      </div>
    </section>
  );
}
