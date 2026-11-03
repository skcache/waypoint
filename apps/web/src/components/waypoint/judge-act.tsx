"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowRight, CaretDown, CaretLeft, CaretRight, Play, Pause } from "@phosphor-icons/react";
import Link from "next/link";

import type { DemoCounterexample, DemoEval, DemoExport, DemoObservation } from "@/lib/demo";
import {
  callTimeline,
  cleanEffect,
  compareInvariants,
  diffNotes,
  effectCount,
  failedIds,
  formatMicros,
  gateSteps,
  orderIdentities,
  verdictWord,
  type Column,
  type CompareRow,
} from "@/lib/evidence";
import { humanStatus, shortId, utcTimestamp } from "@/lib/showcase";
import { CodeViewer } from "./code-viewer";
import type { Candidate } from "./types";
import { ActHeading, Container, CopyChip, Fact, StatusMark, YesNo } from "./ui";

const COLUMNS: { key: Column; label: string; short: string; tab: string }[] = [
  { key: "before", label: "Before the repair", short: "Before", tab: "Original code" },
  { key: "repair", label: "AI repair", short: "Repair", tab: "AI repair" },
  { key: "unsafe", label: "Seeded unsafe patch", short: "Unsafe", tab: "Unsafe seed" },
];

export function JudgeAct({
  doc,
  candidate,
  setCandidate,
}: {
  doc: DemoExport;
  candidate: Candidate;
  setCandidate: (c: Candidate) => void;
}) {
  const evals: Record<Column, DemoEval> = { before: doc.pre_repair, repair: doc.oracle, unsafe: doc.unsafe };
  const ids = orderIdentities(doc);
  const active = evals[candidate];
  const word = verdictWord(active.verdict);
  const count = effectCount(active);
  const failed = failedIds(active);
  const hasUnsafeDiff = Boolean(doc.unsafe.diff);
  const [compare, setCompare] = useState(false);
  const applicable = active.invariants.filter((i) => i.status !== "NOT_APPLICABLE").length;
  const passed = active.invariants.filter((i) => i.status === "PASS").length;
  const captured = doc.pre_repair.provider_capture_id ? 1 : 0;
  const balanced = count !== null && count === captured;

  return (
    <section id="judge" className="night scroll-mt-14">
      <Container className="py-20 sm:py-28">
        <ActHeading title={<>Can the judge be fooled?</>}>
          A separate program re-reads the merchant&rsquo;s ledger after the candidate runs and scores{" "}
          {doc.oracle.invariants.length} invariants. It does not trust the candidate.
        </ActHeading>

        <div className="mt-12 grid grid-cols-3 border-b border-line" role="tablist" aria-label="Candidate under test">
          {COLUMNS.map((col) => {
            const e = evals[col.key];
            const on = candidate === col.key;
            return (
              <button
                key={col.key}
                type="button"
                role="tab"
                id={`tab-${col.key}`}
                aria-selected={on}
                aria-controls="judge-panel"
                data-candidate={col.key}
                onClick={() => setCandidate(col.key)}
                className={`-mb-px flex min-h-[76px] flex-col items-start justify-center gap-1 border-b-2 px-3 py-3 text-left transition-colors sm:px-5 ${
                  on ? "border-accent text-ink" : "border-transparent text-muted hover:text-ink"
                }`}
              >
                <span className="text-base font-semibold leading-tight sm:text-lg">{col.tab}</span>
                <span className={`text-sm font-medium ${verdictWord(e.verdict) === "VERIFIED" ? "text-ok" : "text-bad"}`}>
                  {verdictWord(e.verdict) === "VERIFIED" ? "Verified" : "Unsafe"}
                </span>
              </button>
            );
          })}
        </div>

        <p className="mt-5 flex flex-wrap items-center gap-x-5 gap-y-2 text-[15px] text-faint" aria-label="Orders in play">
          <OrderChip label="Incident order" id={ids.incident} />
          {ids.verification ? <OrderChip label="Verification order" id={ids.verification} /> : null}
          {ids.unsafeRun ? <OrderChip label="Unsafe candidate's order" id={ids.unsafeRun} /> : null}
        </p>

        <div id="judge-panel" role="tabpanel" aria-labelledby={`tab-${candidate}`} className="mt-10 grid gap-10 xl:grid-cols-[minmax(0,.8fr)_minmax(0,1.2fr)] xl:gap-14">
          <div key={candidate} className="swap min-w-0" data-testid={candidate === "unsafe" ? "demo-unsafe" : "demo-oracle"} aria-live="polite">
            <p className="label">Verdict</p>
            <p className={`mt-1 text-[clamp(3rem,6vw,5rem)] font-semibold leading-none tracking-[-0.03em] ${word === "VERIFIED" ? "text-ok" : "text-bad"}`}>
              {word === "VERIFIED" ? "VERIFIED" : word === "UNSAFE" ? "UNSAFE" : "INCONCLUSIVE"}
            </p>
            <p className="mt-3 text-[15px] text-muted">
              {active.verdict !== "UNSAFE" ? <span className="mono">{active.verdict}</span> : null}
              {active.verdict !== "UNSAFE" && active.order_status ? " · " : ""}
              {active.order_status ? `order ${humanStatus(active.order_status).toLowerCase()}` : ""}
            </p>

            <div className="mt-8 rounded-panel bg-surface">
              <dl className="grid grid-cols-2 gap-4 px-5 pt-5">
                <div>
                  <dt className="label">Captured</dt>
                  <dd className="mt-1 text-3xl font-semibold tabular-nums">{captured}</dd>
                </div>
                <div>
                  <dt className="label">Fulfilled</dt>
                  <dd className={`mt-1 text-3xl font-semibold tabular-nums ${balanced ? "text-ok" : "text-bad"}`}>{count ?? "not stated"}</dd>
                </div>
              </dl>
              <div className="double-rule mx-5 mt-4" aria-hidden />
              <p className="px-5 py-4 text-[15px] text-muted">
                {balanced ? "The statement balances." : "The statement does not balance."}{" "}
                {word === "VERIFIED" ? `${passed} of ${applicable} applicable invariants pass.` : `${failed.length} invariant${failed.length === 1 ? "" : "s"} fail${failed.length === 1 ? "s" : ""}: ${failed.join(", ")}.`}
              </p>
            </div>

            <p className="mt-6 text-[17px] leading-[1.6]">{active.reason}</p>
            {active.evaluated_at ? <p className="mt-2 text-[15px] text-faint">Evaluated {utcTimestamp(active.evaluated_at)}</p> : null}
            {candidate === "unsafe" ? (
              <p className="mt-4 text-[15px] leading-[1.6] text-muted">
                Historical independent-oracle evidence. {doc.unsafe.provenance}{" "}
                {doc.unsafe.fresh_trusted_gate.executed
                  ? `Fresh trusted gate: ${humanStatus(doc.unsafe.fresh_trusted_gate.result)}. ${doc.unsafe.fresh_trusted_gate.detail ?? ""}`
                  : "The fresh trusted gate was not run for this recorded artifact."}
              </p>
            ) : null}
            {candidate === "repair" ? (
              <p className="mt-4 text-[15px] leading-[1.6] text-muted">
                Verified for the tested scenario on a tier {doc.run.evidence_tier} scripted fixture. This is not a claim about live PayPal
                behavior or scenarios that were not run.
              </p>
            ) : null}
            <ContrastNote doc={doc} candidate={candidate} />
          </div>

          <div className="min-w-0 space-y-4" data-testid={candidate === "unsafe" ? "demo-unsafe-code" : "demo-code"}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="label">Code under test</h3>
              {doc.patch.diff && hasUnsafeDiff ? (
                <button
                  type="button"
                  aria-pressed={compare}
                  onClick={() => setCompare((v) => !v)}
                  className="inline-flex min-h-11 items-center rounded border border-line-strong px-4 text-[15px] font-medium transition-colors hover:border-ink aria-pressed:bg-surface"
                >
                  {compare ? "Show selected only" : "Compare both patches"}
                </button>
              ) : null}
            </div>
            {compare && hasUnsafeDiff ? (
              <div className="grid gap-4 2xl:grid-cols-2">
                <CodeViewer heading="AI repair" file={doc.patch.changed_files[0] ?? "patch"} diff={doc.patch.diff} digest={doc.patch.digest} notes={diffNotes(doc.patch.digest)} />
                <CodeViewer testId="demo-unsafe-diff" heading="Seeded unsafe patch" file={doc.unsafe.changed_files?.[0] ?? "patch"} diff={doc.unsafe.diff ?? ""} digest={doc.unsafe.diff_digest} notes={diffNotes(doc.unsafe.diff_digest)} />
              </div>
            ) : candidate === "repair" ? (
              <CodeViewer heading="AI repair" file={doc.patch.changed_files[0] ?? "patch"} diff={doc.patch.diff} digest={doc.patch.digest} notes={diffNotes(doc.patch.digest)} />
            ) : candidate === "unsafe" && hasUnsafeDiff ? (
              <CodeViewer testId="demo-unsafe-diff" heading="Seeded unsafe patch" file={doc.unsafe.changed_files?.[0] ?? "patch"} diff={doc.unsafe.diff ?? ""} digest={doc.unsafe.diff_digest} notes={diffNotes(doc.unsafe.diff_digest)} />
            ) : (
              <p className="rounded-panel bg-surface p-6 text-base leading-[1.6] text-muted">
                {candidate === "before"
                  ? "No patch: this is the incident as shipped. The lines either patch removes are the original code."
                  : "This recorded evaluation exported no patch for the unsafe candidate."}
              </p>
            )}
          </div>
        </div>

        <Matrix doc={doc} candidate={candidate} />
        <Gate doc={doc} />
        <Calls doc={doc} />

        <div className="mt-20 flex flex-col items-start justify-between gap-4 border-t border-line pt-8 sm:flex-row sm:items-center">
          <p className="max-w-[56ch] text-base leading-[1.6] text-muted">
            One incident is a demonstration, not a result. The held-out benchmark reports how this pipeline did across six frozen
            incidents, including the ones it has not run.
          </p>
          <Link href="/benchmark" className="group inline-flex h-12 shrink-0 items-center gap-2 rounded border border-line-strong px-5 text-base font-medium transition-colors hover:border-ink">
            Held-out benchmark (preliminary)
            <ArrowRight size={16} weight="bold" aria-hidden />
          </Link>
        </div>
      </Container>
    </section>
  );
}

function ContrastNote({ doc, candidate }: { doc: DemoExport; candidate: Candidate }) {
  if (candidate === "before") return null;
  const unsafeFails = failedIds(doc.unsafe);
  const clearedByBoth = failedIds(doc.pre_repair).filter((id) => !unsafeFails.includes(id));
  if (!clearedByBoth.length || !unsafeFails.length) return null;
  return (
    <p className="mt-6 border-t border-line pt-5 text-[17px] leading-[1.6]">
      <b className="font-semibold">What the comparison shows.</b> The seeded unsafe patch clears {clearedByBoth.join(", ")} but still fails{" "}
      {unsafeFails.join(", ")}. A patch can silence the visible symptom and leave the money problem in place. Only the repair passes all of them.
    </p>
  );
}

function OrderChip({ label, id }: { label: string; id: string }) {
  return (
    <span className="inline-flex items-center gap-2">
      <span>{label}</span>
      <CopyChip value={id} label={shortId(id, 8, 4)} />
    </span>
  );
}

function counterFacts(c: DemoCounterexample | undefined, ids: ReturnType<typeof orderIdentities>) {
  if (!c) return [];
  const out: { label: string; value: string; note?: string }[] = [];
  if (c.order_id) {
    const which = c.order_id === ids.incident ? "incident order" : c.order_id === ids.verification ? "verification order" : c.order_id === ids.unsafeRun ? "unsafe candidate's order" : "order";
    out.push({ label: "Order", value: c.order_id, note: which });
  }
  if (c.final_status) out.push({ label: "Final status", value: humanStatus(c.final_status) });
  if (c.confirm_event_id !== undefined) out.push({ label: "Journal confirmation", value: `event #${c.confirm_event_id}` });
  if (c.effect_count !== undefined) out.push({ label: "Effects recorded", value: String(c.effect_count) });
  if (c.effects?.length) out.push({ label: "Effect keys", value: c.effects.map(cleanEffect).join(", ") });
  if (c.event_id) out.push({ label: "Backing event", value: c.event_id });
  return out;
}

function Matrix({ doc, candidate }: { doc: DemoExport; candidate: Candidate }) {
  const rows = useMemo(() => compareInvariants(doc), [doc]);
  const differing = rows.filter((r) => r.differs);
  const [onlyDiff, setOnlyDiff] = useState(true);
  const [open, setOpen] = useState<string | null>(differing.find((r) => r.id === "I13")?.id ?? differing[0]?.id ?? null);
  const ids = orderIdentities(doc);
  const shown = onlyDiff ? differing : rows;

  return (
    <div className="mt-16" data-testid="invariant-matrix">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h3 className="text-2xl font-semibold tracking-[-0.02em]">Where the three candidates disagree</h3>
        </div>
        <div className="inline-flex rounded border border-line p-1" role="group" aria-label="Row filter">
          {[{ v: true, t: `Differences (${differing.length})` }, { v: false, t: `All ${rows.length}` }].map((o) => (
            <button
              key={String(o.v)}
              type="button"
              aria-pressed={onlyDiff === o.v}
              onClick={() => setOnlyDiff(o.v)}
              className="min-h-10 rounded-sm px-4 text-[15px] font-medium text-muted transition-colors aria-pressed:bg-ink aria-pressed:text-background"
            >
              {o.t}
            </button>
          ))}
        </div>
      </div>

      <div className="mt-6 overflow-hidden rounded-panel bg-surface">
        <div className="flex items-end gap-3 border-b border-line px-4 py-3">
          <span className="label flex-1">Invariant</span>
          {COLUMNS.map((c) => (
            <span key={c.key} className={`label w-14 text-center sm:w-20 ${c.key === candidate ? "!font-semibold !text-ink underline decoration-accent decoration-2 underline-offset-4" : ""}`}>{c.short}</span>
          ))}
          <span className="w-5" aria-hidden />
        </div>
        <ul>
          {shown.map((row) => (
            <MatrixRow key={row.id} row={row} candidate={candidate} open={open === row.id} onToggle={() => setOpen(open === row.id ? null : row.id)} ids={ids} />
          ))}
        </ul>
      </div>
      <p className="mt-3 text-[15px] text-faint">N/A means the invariant does not apply to the scenario that ran. Select a row for each candidate&rsquo;s counterexample.</p>
    </div>
  );
}

function MatrixRow({ row, candidate, open, onToggle, ids }: { row: CompareRow; candidate: Candidate; open: boolean; onToggle: () => void; ids: ReturnType<typeof orderIdentities> }) {
  const cells: [Column, CompareRow["before"]][] = [["before", row.before], ["repair", row.repair], ["unsafe", row.unsafe]];
  return (
    <li className="border-b border-line last:border-0">
      <button type="button" onClick={onToggle} aria-expanded={open} className={`flex w-full items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-sunk ${row.differs ? "" : "opacity-80"}`}>
        <span className="flex min-w-0 flex-1 items-baseline gap-3">
          <span className="mono w-9 shrink-0 text-base font-semibold">{row.id}</span>
          <span className="line-clamp-2 text-sm leading-5 text-muted">{row.summary}</span>
        </span>
        {cells.map(([key, inv]) => (
          <span key={key} className="flex w-14 justify-center py-1 sm:w-20">
            <StatusMark status={inv?.status} compact />
          </span>
        ))}
        <CaretDown size={16} weight="bold" className={`w-5 shrink-0 text-faint transition-transform ${open ? "rotate-180" : ""}`} aria-hidden />
      </button>
      {open ? (
        <div className="grid gap-3 border-t border-line px-4 py-4 md:grid-cols-3">
          {cells.map(([key, inv]) => {
            const facts = counterFacts(inv?.counterexample, ids);
            return (
              <div key={key} className={`rounded-panel bg-background p-4 ${key === candidate ? "outline outline-1 outline-accent" : ""}`}>
                <div className="flex items-center justify-between gap-2">
                  <p className="text-[15px] font-medium">{COLUMNS.find((c) => c.key === key)?.label}</p>
                  <StatusMark status={inv?.status} />
                </div>
                {facts.length ? (
                  <dl className="mt-2 divide-y divide-line">
                    {facts.map((f) => (
                      <Fact key={f.label} label={f.label}><span className="mono break-all">{f.value}</span>{f.note ? <span className="mt-0.5 block text-sm text-faint">{f.note}</span> : null}</Fact>
                    ))}
                  </dl>
                ) : (
                  <p className="mt-2 text-sm leading-6 text-muted">{inv?.status === "PASS" ? "No counterexample: the invariant held." : inv?.status === "FAIL" ? "No counterexample fields exported." : "Not applicable to this scenario."}</p>
                )}
              </div>
            );
          })}
        </div>
      ) : null}
    </li>
  );
}

function Gate({ doc }: { doc: DemoExport }) {
  const steps = gateSteps(doc.verification.steps);
  const parts = (doc.verification.sandbox ?? "").split(";").map((s) => s.trim()).filter(Boolean);
  return (
    <div className="mt-20">
      <h3 className="text-2xl font-semibold tracking-[-0.02em]">The repaired candidate ran in a disposable sandbox</h3>
      <ol className="mt-6 divide-y divide-line rounded-panel bg-surface" data-testid="gate-steps">
        {steps.map((s) => (
          <li key={s.name} className="grid grid-cols-[5.5rem_1fr_auto] items-center gap-x-4 gap-y-1 px-5 py-4 md:grid-cols-[8rem_1fr_7rem_6rem]">
            <span className="mono text-base">{s.name}</span>
            <span className="col-span-3 col-start-1 row-start-2 h-1.5 overflow-hidden rounded-sm bg-sunk md:col-span-1 md:col-start-2 md:row-start-1">
              <span className={`block h-full rounded-sm ${s.passed ? "bg-ok" : "bg-bad"}`} style={{ width: `${Math.max(3, s.share * 100)}%` }} />
            </span>
            <span className="text-right text-[15px] tabular-nums text-muted">{s.duration_ms.toLocaleString("en-US")} ms</span>
            <span className="justify-self-end"><StatusMark status={s.passed ? "PASS" : "FAIL"} /></span>
          </li>
        ))}
      </ol>
      <p className="mt-3 text-[15px] text-faint">Bars are scaled to the slowest step; durations are as recorded in the run. Command output stays in the local artifact directory and is not part of the public export.</p>
      {parts.length ? (
        <details className="group mt-4 border-y border-line">
          <summary className="flex min-h-12 items-center text-base font-medium">Isolation the candidate ran under</summary>
          <ul className="grid gap-x-8 pb-4 sm:grid-cols-2">
            {parts.map((p) => (
              <li key={p} className="mono py-1 text-[15px] leading-6 text-muted">{p}</li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}

function Calls({ doc }: { doc: DemoExport }) {
  const nodes = useMemo(() => callTimeline(doc), [doc]);
  const captureIdx = nodes.findIndex((n) => n.obs.capture_id);
  const [idx, setIdx] = useState(Math.max(0, captureIdx));
  const [playing, setPlaying] = useState(false);
  const verification = doc.incident.verification_order_id;

  useEffect(() => {
    if (!playing || idx >= nodes.length - 1) return;
    const id = window.setTimeout(() => {
      const next = idx + 1;
      setIdx(next);
      if (next >= nodes.length - 1) setPlaying(false);
    }, 1000);
    return () => window.clearTimeout(id);
  }, [playing, idx, nodes.length]);

  if (!nodes.length) return null;
  const obs: DemoObservation = nodes[idx].obs;
  const rows: [string, React.ReactNode][] = [
    ["Order status", humanStatus(obs.order_status)],
    ["Provider order ref", <span key="o" className="mono">{obs.order_ref ?? "not recorded"}</span>],
    ["Request id", obs.request_id ? <CopyChip key="r" value={obs.request_id} label={shortId(obs.request_id, 8, 4)} /> : "not recorded"],
    ["Durable before call", <YesNo key="d" value={obs.request_id_durable_before_call} />],
    ["Sent amount", obs.sent_amount ? `${obs.sent_amount} ${obs.sent_currency ?? ""}` : "not recorded"],
    ["Mutation applied", <YesNo key="m" value={obs.mutation_applied} />],
    ["Idempotent replay", <YesNo key="i" value={obs.idempotent_replay} />],
    ["Response dropped", <YesNo key="x" value={obs.response_dropped} />],
    ...(obs.capture_id
      ? ([["Capture", <span key="c" className="mono">{obs.capture_id} · {obs.capture_status} · {obs.capture_amount} {obs.capture_currency}</span>]] as [string, React.ReactNode][])
      : []),
    ["Source", <span key="s" className="mono">{obs.source ?? "not recorded"} · tier {obs.tier ?? "?"}</span>],
  ];

  return (
    <div className="mt-20" data-testid="provider-calls">
      <h3 className="text-2xl font-semibold tracking-[-0.02em]">What the repaired candidate asked the provider</h3>
      <p className="mt-2 max-w-[66ch] text-base leading-[1.6] text-muted">
        These {nodes.length} calls belong to the verification order{verification ? ` ${shortId(verification, 8, 4)}` : ""}, not the incident order. Real gaps are shown; stepping is not to scale.
      </p>

      <div className="mt-6 rounded-panel bg-surface p-5 sm:p-6">
        <div className="flex items-center gap-3">
          <button
            type="button"
            aria-label="Previous call"
            disabled={idx === 0}
            onClick={() => { setPlaying(false); setIdx((i) => Math.max(0, i - 1)); }}
            className="grid h-11 w-11 place-items-center rounded border border-line-strong disabled:opacity-35"
          >
            <CaretLeft size={16} weight="bold" aria-hidden />
          </button>
          <button
            type="button"
            onClick={() => { if (idx >= nodes.length - 1) setIdx(0); setPlaying((p) => !p); }}
            className="inline-flex h-11 items-center gap-2 rounded bg-ink px-5 text-[15px] font-medium text-background"
          >
            {playing ? <Pause size={15} weight="fill" aria-hidden /> : <Play size={15} weight="fill" aria-hidden />}
            {playing ? "Pause" : "Step through"}
          </button>
          <button
            type="button"
            aria-label="Next call"
            disabled={idx >= nodes.length - 1}
            onClick={() => { setPlaying(false); setIdx((i) => Math.min(nodes.length - 1, i + 1)); }}
            className="grid h-11 w-11 place-items-center rounded border border-line-strong disabled:opacity-35"
          >
            <CaretRight size={16} weight="bold" aria-hidden />
          </button>
          <span className="ml-auto text-[15px] text-muted">{idx + 1} of {nodes.length}</span>
        </div>

        <ol className="relative mt-7 grid" style={{ gridTemplateColumns: `repeat(${nodes.length}, minmax(0, 1fr))` }} aria-label="Provider calls">
          <span className="absolute left-[calc(50%/var(--n))] right-[calc(50%/var(--n))] top-[19px] h-px bg-line-strong" style={{ ["--n" as string]: nodes.length }} aria-hidden />
          {nodes.map((n, i) => {
            const reached = i <= idx;
            const on = i === idx;
            return (
              <li key={n.obs.seq} className="relative flex flex-col items-center">
                <button
                  type="button"
                  data-testid="observation-row"
                  aria-pressed={on}
                  aria-label={`Call ${n.obs.seq}: ${n.obs.method}`}
                  onClick={() => { setPlaying(false); setIdx(i); }}
                  className={`relative z-[1] grid h-10 w-10 place-items-center rounded border-2 transition-colors ${on ? "border-accent bg-accent text-on-accent" : reached ? "border-ink bg-ink text-background" : "border-line-strong bg-surface text-muted"}`}
                >
                  <span className="text-[15px] font-semibold">{n.obs.seq}</span>
                </button>
                <span className={`mono mt-2 text-[15px] font-medium ${on ? "text-ink" : "text-muted"}`}>{n.obs.method}</span>
                <span className="text-[13px] text-faint">{i === 0 ? "start" : `+${formatMicros(n.gapUs)}`}</span>
              </li>
            );
          })}
        </ol>

        <dl className="mt-6 divide-y divide-line border-t border-line" aria-live="polite">
          {rows.map(([label, value]) => (
            <Fact key={label} label={label}>{value}</Fact>
          ))}
        </dl>
      </div>
    </div>
  );
}
