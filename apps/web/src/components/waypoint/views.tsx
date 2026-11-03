"use client";

import { useMemo, useState } from "react";
import { ArrowRight } from "@phosphor-icons/react";

import type { DemoExport, DemoInvariant, DemoObservation } from "@/lib/demo";
import {
  callTimeline,
  cleanEffect,
  compareInvariants,
  effectCount,
  failedIds,
  firstSentence,
  formatMicros,
  formatMs,
  ledgerAutopsy,
  orderIdentities,
  totalStepMs,
  verdictWord,
  type Column,
} from "@/lib/evidence";
import { humanStatus, money, shortId, titleFor, utcTimestamp } from "@/lib/showcase";
import { Disclosure, Fact, OutlineButton, Numeral, RailGroup, RailRow, CopyChip, YesNo, type Tone } from "./kit";
import { PatchSheet } from "./patch-sheet";
import { TimeMap } from "./time-map";
import type { Candidate, EvidenceSource, View } from "./types";
import { VerdictMap } from "./verdict-map";

type Common = { doc: DemoExport; go: (v: View) => void; part: "head" | "body" };

const Headline = ({ children }: { children: React.ReactNode }) => (
  <h1 className="mt-7 max-w-[24ch] text-balance text-[22px] font-semibold leading-[1.25] tracking-[-0.01em]">{children}</h1>
);
const Lead = ({ children }: { children: React.ReactNode }) => <p className="mt-3 max-w-[44ch] text-[16px] leading-[1.55] text-muted">{children}</p>;
const Caption = ({ numeral, lines }: { numeral: React.ReactNode; lines: React.ReactNode }) => (
  <div className="mt-6 flex items-end gap-5">
    {numeral}
    <div className="mb-1.5 min-w-0 space-y-1.5 text-[15px] leading-snug">{lines}</div>
  </div>
);
const Line = ({ tone, children }: { tone?: Tone; children: React.ReactNode }) => (
  <p className="flex items-start gap-2 text-muted">
    {tone ? <span className="mt-1 inline-block"><ToneDot tone={tone} /></span> : null}
    <span>{children}</span>
  </p>
);
function ToneDot({ tone }: { tone: Tone }) {
  const c = tone === "fail" ? "var(--c-fail)" : tone === "ok" ? "var(--c-pass)" : tone === "warn" ? "var(--c-warn)" : "var(--c-faint)";
  return <span className="block h-2.5 w-2.5 rounded-full" style={{ background: c }} aria-hidden />;
}

function InvariantDetail({ doc, id, column }: { doc: DemoExport; id: string; column: Column }) {
  const rows = useMemo(() => compareInvariants(doc), [doc]);
  const row = rows.find((r) => r.id === id);
  const inv: DemoInvariant | undefined = row?.[column === "before" ? "before" : column === "repair" ? "repair" : "unsafe"];
  const ids = orderIdentities(doc);
  if (!row || !inv) return null;
  const c = inv.counterexample;
  const which = (o?: string) => (o === ids.incident ? "incident order" : o === ids.verification ? "verification order" : o === ids.unsafeRun ? "unsafe candidate's order" : "order");
  return (
    <RailGroup title={`${inv.id} · ${inv.title ?? inv.summary}`} aside={<span className={inv.status === "FAIL" ? "text-bad" : ""}>{inv.status === "FAIL" ? "Fail" : inv.status === "PASS" ? "Pass" : "N/A"}</span>}>
      <p className="mt-2 text-[15px] leading-[1.6] text-muted" data-testid="invariant-detail">{inv.summary}</p>
      {c ? (
        <dl className="mt-3 border-t border-line">
          {c.order_id ? <Fact label="Order"><span className="mono break-all">{c.order_id}</span><span className="block text-sm text-faint">{which(c.order_id)}</span></Fact> : null}
          {c.final_status ? <Fact label="Final status">{humanStatus(c.final_status)}</Fact> : null}
          {c.confirm_event_id !== undefined ? <Fact label="Journal confirmation">event #{c.confirm_event_id}</Fact> : null}
          {c.effect_count !== undefined ? <Fact label="Effects recorded">{c.effect_count}</Fact> : null}
          {c.effects?.length ? <Fact label="Effect keys"><span className="mono">{c.effects.map(cleanEffect).join(", ")}</span></Fact> : null}
          {c.event_id ? <Fact label="Backing event"><span className="mono">{c.event_id}</span></Fact> : null}
        </dl>
      ) : null}
    </RailGroup>
  );
}

/* ---------------------------------------------------------------- Break */

export function BreakRail({ doc, go, part, inv, setInv, source }: Common & { inv: string; setInv: (id: string) => void; source: EvidenceSource }) {
  const rows = useMemo(() => ledgerAutopsy(doc.pre_repair), [doc.pre_repair]);
  const count = effectCount(doc.pre_repair);
  const failed = failedIds(doc.pre_repair);
  const webhookRows = doc.incident.observations.some((o) => /webhook/i.test(o.method));
  const head = (
    <>
      <Caption
        numeral={<Numeral value={count ?? "?"} />}
        lines={
          <>
            <p className="text-ink">Fulfillments for one {money(doc.incident.total_minor, doc.incident.currency)} payment</p>
            <Line tone="fail">Judge flagged {failed.join(" · ") || "nothing"}</Line>
            <Line>
              Case file · {titleFor(doc)} · {source === "recorded" ? "recorded" : "local run"}
            </Line>
          </>
        }
      />
      <Headline>An AI fixed a payment bug. A separate judge checked the ledger.</Headline>
    </>
  );
  const body = (
    <>
      <Lead>This is the incident order as the judge read it, before any repair. Select a cell on the map or an entry below.</Lead>

      <RailGroup title="Ledger statement" aside={money(doc.incident.total_minor, doc.incident.currency)}>
        {rows.map((row) => (
          <RailRow
            key={row.key}
            testId="ledger-row"
            tone={row.state === "fail" ? "fail" : "ok"}
            label={row.title}
            sub={<span className="mono">{row.value}</span>}
            value={row.invariantIds.join(" · ")}
            selected={row.invariantIds.includes(inv)}
            onClick={() => setInv(row.invariantIds[0] ?? inv)}
          />
        ))}
        <p className="mt-2 text-sm leading-5 text-faint">
          Captured 1, fulfilled {count ?? "?"}: one capture cannot support more than one fulfillment, so the statement does not balance.
          Entries are listed, not timed.
        </p>
      </RailGroup>

      <InvariantDetail doc={doc} id={inv} column="before" />

      {!webhookRows ? (
        <RailGroup title="Limits">
          <Disclosure summary="What this evidence cannot show" tone="warn">
            The export has no webhook-delivery rows, and its {doc.incident.observations.length} provider calls belong to the later verification
            order. How the second effect arrived, and when, was not observed. The AI&rsquo;s explanation in Fix is a hypothesis about that.
          </Disclosure>
        </RailGroup>
      ) : null}
      <div className="mt-6 pb-2">
        <OutlineButton onClick={() => go("fix")}>
          Next: Fix <ArrowRight size={15} aria-hidden />
        </OutlineButton>
      </div>
    </>
  );
  return part === "head" ? head : body;
}

export function BreakCanvas({ doc, inv, setInv }: { doc: DemoExport; inv: string; setInv: (id: string) => void }) {
  return <VerdictMap doc={doc} candidate="before" selected={inv} onSelect={setInv} />;
}

/* ------------------------------------------------------------------ Fix */

export function FixRail({ doc, go, part, setCandidate }: Common & { setCandidate: (c: Candidate) => void }) {
  const d = doc.diagnosis;
  const files = doc.patch.changed_files;
  const added = doc.patch.diff.split("\n").filter((l) => l.startsWith("+") && !l.startsWith("+++")).length;
  const removed = doc.patch.diff.split("\n").filter((l) => l.startsWith("-") && !l.startsWith("---")).length;
  const seconds = (ms?: number) => (ms ? `${(ms / 1000).toFixed(1)} s` : "–");
  const head = (
    <>
      <Caption
        numeral={<Numeral value={files.length} />}
        lines={
          <>
            <p className="text-ink">{files.length === 1 ? "File" : "Files"} rewritten by the AI</p>
            <Line tone="ok">
              +{added} −{removed} lines
            </Line>
            <Line tone="warn">Recorded model output</Line>
          </>
        }
      />
      <Headline>The AI read the evidence and wrote this patch.</Headline>
    </>
  );
  const body = (
    <>
      <Lead>Read the diagnosis and the exact diff. The next step shows how the judge scored it.</Lead>

      {d ? (
        <>
          <RailGroup title="What it concluded">
            <p className="mt-2 text-[16px] leading-[1.55] text-ink">{firstSentence(d.root_cause, 420)}</p>
          </RailGroup>
          <div className="mt-3">
            <Disclosure summary="Read the full explanation">
              <p><b className="font-semibold text-ink">Cause.</b> {d.root_cause}</p>
              <p className="mt-3"><b className="font-semibold text-ink">Mechanism.</b> {d.mechanism}</p>
              <p className="mt-3"><b className="font-semibold text-ink">Proposed repair.</b> {d.proposed_fix}</p>
            </Disclosure>
            {d.supporting_evidence.length ? (
              <Disclosure summary={`Evidence it cited (${d.supporting_evidence.length})`}>
                <p className="mono">{d.supporting_evidence.join(" · ")}</p>
                <p className="mt-1 text-sm text-faint">Ids from the evidence bundle the model was given.</p>
              </Disclosure>
            ) : null}
            <Disclosure summary={`Files it suspected (${d.suspected_files.length})`}>
              <ul>
                {d.suspected_files.map((f) => (
                  <li key={f} className="py-1">
                    <span className="mono break-all">{f}</span>
                    <span className="block text-sm text-faint">{files.includes(f) ? "changed by the patch" : "suspected, left unchanged"}</span>
                  </li>
                ))}
              </ul>
            </Disclosure>
            {d.uncertainties.length ? (
              <Disclosure summary="What the model says it cannot know" tone="warn" open>
                <p>{d.uncertainties[0]}</p>
                {d.uncertainties.length > 1 ? (
                  <ul className="mt-3 list-disc space-y-2 pl-5">
                    {d.uncertainties.slice(1).map((u) => <li key={u}>{u}</li>)}
                  </ul>
                ) : null}
              </Disclosure>
            ) : null}
          </div>
        </>
      ) : (
        <p className="mt-6 text-muted">This run exported no structured diagnosis.</p>
      )}

      <RailGroup title="Model" aside={<span className="mono text-[13px]">{doc.run.model}</span>}>
        {doc.run.model_calls.map((c) => (
          <RailRow key={c.purpose} tone="idle" label={c.purpose} sub={`${(c.prompt_tokens ?? 0).toLocaleString("en-US")} in · ${(c.completion_tokens ?? 0).toLocaleString("en-US")} out`} value={seconds(c.latency_ms)} />
        ))}
        <p className="mt-2 text-sm leading-5 text-faint">Recorded {utcTimestamp(doc.run.model_recorded_at ?? doc.run.finished_at)}. {doc.run.kind === "replay" ? "Replayed recorded model output." : "Recorded hosted-model reply."} Applied only to a disposable worktree.</p>
      </RailGroup>

      <div className="mt-6 flex flex-wrap gap-3 pb-2">
        <OutlineButton onClick={() => go("judge")}>
          Next: Judge <ArrowRight size={15} aria-hidden />
        </OutlineButton>
        <OutlineButton onClick={() => { setCandidate("unsafe"); go("judge"); }}>Try to fool the judge</OutlineButton>
      </div>
    </>
  );
  return part === "head" ? head : body;
}

export function FixCanvas({ doc }: { doc: DemoExport }) {
  return <PatchSheet doc={doc} candidate="repair" />;
}

/* ---------------------------------------------------------------- Judge */

const NAMES: Record<Candidate, { name: string; short: string }> = {
  before: { name: "Original code", short: "before" },
  repair: { name: "AI repair", short: "repair" },
  unsafe: { name: "Seeded unsafe patch", short: "unsafe" },
};

export function JudgeRail({
  doc,
  go,
  part,
  candidate,
  setCandidate,
  inv,
  mode,
  setMode,
  differences,
  setDifferences,
}: Common & {
  candidate: Candidate;
  setCandidate: (c: Candidate) => void;
  inv: string;
  mode: "map" | "code";
  setMode: (m: "map" | "code") => void;
  differences: boolean;
  setDifferences: (v: boolean) => void;
}) {
  const evals = { before: doc.pre_repair, repair: doc.oracle, unsafe: doc.unsafe };
  const active = evals[candidate];
  const word = verdictWord(active.verdict);
  const count = effectCount(active);
  const failed = failedIds(active);
  const applicable = active.invariants.filter((i) => i.status !== "NOT_APPLICABLE").length;
  const passed = active.invariants.filter((i) => i.status === "PASS").length;
  const captured = doc.pre_repair.provider_capture_id ? 1 : 0;
  const ids = orderIdentities(doc);
  const diffCount = compareInvariants(doc).filter((r) => r.differs).length;
  const head = (
    <>
      <Caption
        numeral={
          <p key={candidate} className={`swap numeral text-[clamp(3.2rem,6.4vw,4.6rem)] ${word === "VERIFIED" ? "text-ink" : "text-bad"}`} data-testid={candidate === "unsafe" ? "demo-unsafe" : "demo-oracle"}>
            {word === "VERIFIED" ? "Verified" : word === "UNSAFE" ? "Unsafe" : "Inconclusive"}
          </p>
        }
        lines={
          <>
            <p className="text-ink">
              {count ?? "?"} fulfillment{count === 1 ? "" : "s"} for {captured} capture
            </p>
            <Line tone={failed.length ? "fail" : "ok"}>
              {word === "VERIFIED" ? `${passed} of ${applicable} applicable invariants pass` : `Fails ${failed.join(" · ")}`}
            </Line>
          </>
        }
      />
      <Headline>Can the judge be fooled?</Headline>
    </>
  );
  const body = (
    <>
      <Lead>A separate program re-reads the merchant&rsquo;s ledger after the candidate runs and scores {doc.oracle.invariants.length} invariants. It does not trust the candidate.</Lead>

      <RailGroup title="Candidate under test">
        <div role="radiogroup" aria-label="Candidate under test">
          {(Object.keys(NAMES) as Candidate[]).map((key) => {
            const e = evals[key];
            const w = verdictWord(e.verdict);
            const n = effectCount(e);
            return (
              <button
                key={key}
                type="button"
                role="radio"
                aria-checked={candidate === key}
                data-candidate={key}
                data-testid={`candidate-${key}`}
                onClick={() => setCandidate(key)}
                className={`grid min-h-14 w-full grid-cols-[16px_1fr_auto] items-center gap-3 border-b border-line px-1 py-2 text-left transition-colors hover:bg-cell-dark ${candidate === key ? "bg-cell-dark" : ""}`}
              >
                <ToneDotGlyph ok={w === "VERIFIED"} />
                <span className="min-w-0">
                  <span className="block text-[15px] text-ink">{NAMES[key].name}</span>
                  <span className="block truncate text-sm text-faint">{failedIds(e).length ? `fails ${failedIds(e).join(", ")}` : "no invariant fails"}</span>
                </span>
                <span className="text-[15px] tabular-nums text-muted">×{n ?? "?"}</span>
              </button>
            );
          })}
        </div>
      </RailGroup>

      <div className="mt-4 flex flex-wrap gap-2">
        <OutlineButton pressed={mode === "map"} onClick={() => setMode("map")}>Map</OutlineButton>
        <OutlineButton pressed={mode === "code"} onClick={() => setMode("code")}>Code</OutlineButton>
        {mode === "map" ? (
          <OutlineButton pressed={differences} onClick={() => setDifferences(!differences)}>
            {differences ? "Showing differences" : `Show differences (${diffCount})`}
          </OutlineButton>
        ) : null}
      </div>

      <p className="mt-5 text-[15px] leading-[1.6] text-ink">{active.reason}</p>
      {active.evaluated_at ? <p className="mt-1 text-sm text-faint">Evaluated {utcTimestamp(active.evaluated_at)}</p> : null}
      {candidate === "unsafe" ? (
        <p className="mt-3 text-sm leading-[1.6] text-muted">
          Historical independent-oracle evidence. {doc.unsafe.provenance}{" "}
          {doc.unsafe.fresh_trusted_gate.executed
            ? `Fresh trusted gate: ${humanStatus(doc.unsafe.fresh_trusted_gate.result)}. ${doc.unsafe.fresh_trusted_gate.detail ?? ""}`
            : "The fresh trusted gate was not run for this recorded artifact."}
        </p>
      ) : null}
      {candidate === "repair" ? (
        <p className="mt-3 text-sm leading-[1.6] text-muted">
          Verified for the tested scenario on a tier {doc.run.evidence_tier} scripted fixture. This is not a claim about live PayPal behavior or scenarios that were not run.
        </p>
      ) : null}
      {candidate !== "before" && failedIds(doc.unsafe).length ? (
        <p className="mt-3 text-[15px] leading-[1.6] text-ink">
          <b className="font-semibold">The comparison.</b> The unsafe patch clears {failedIds(doc.pre_repair).filter((i) => !failedIds(doc.unsafe).includes(i)).join(", ")} but still fails {failedIds(doc.unsafe).join(", ")}: it silences the visible symptom and leaves the money problem in place.
        </p>
      ) : null}

      <InvariantDetail doc={doc} id={inv} column={candidate} />

      <RailGroup title="Orders in play">
        <div className="mt-2 flex flex-wrap gap-2">
          <OrderChip label="Incident" id={ids.incident} />
          {ids.verification ? <OrderChip label="Verification" id={ids.verification} /> : null}
          {ids.unsafeRun ? <OrderChip label="Unsafe run" id={ids.unsafeRun} /> : null}
        </div>
      </RailGroup>

      <div className="mt-6 pb-2">
        <OutlineButton onClick={() => go("evidence")}>
          Next: Evidence <ArrowRight size={15} aria-hidden />
        </OutlineButton>
      </div>
    </>
  );
  return part === "head" ? head : body;
}

function ToneDotGlyph({ ok }: { ok: boolean }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden className="shrink-0">
      {ok ? (
        <>
          <circle cx="8" cy="8" r="6.7" fill="none" stroke="var(--c-text)" strokeWidth="1.3" />
          <path d="M5 8.2l2 2 4-4.2" stroke="var(--c-text)" strokeWidth="1.4" fill="none" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : (
        <>
          <circle cx="8" cy="8" r="7" fill="var(--c-fail)" />
          <path d="M5.4 5.4l5.2 5.2M10.6 5.4l-5.2 5.2" stroke="var(--cell-cream)" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
    </svg>
  );
}

function OrderChip({ label, id }: { label: string; id: string }) {
  return (
    <span className="inline-flex items-center gap-2 text-sm text-faint">
      {label}
      <CopyChip value={id} label={shortId(id, 8, 4)} />
    </span>
  );
}

export function JudgeCanvas({
  doc,
  candidate,
  inv,
  setInv,
  mode,
  differences,
}: {
  doc: DemoExport;
  candidate: Candidate;
  inv: string;
  setInv: (id: string) => void;
  mode: "map" | "code";
  differences: boolean;
}) {
  return mode === "map" ? (
    <VerdictMap doc={doc} candidate={candidate} selected={inv} onSelect={setInv} differences={differences} />
  ) : (
    <PatchSheet doc={doc} candidate={candidate} />
  );
}

/* ------------------------------------------------------------- Evidence */

export function EvidenceRail({ doc, go, part, step, setStep }: Common & { step: string; setStep: (s: string) => void }) {
  const nodes = useMemo(() => callTimeline(doc), [doc]);
  const [call, setCall] = useState(() => Math.max(0, nodes.findIndex((n) => n.obs.capture_id)));
  const steps = doc.verification.steps;
  const allPass = steps.every((s) => s.passed);
  const ids = orderIdentities(doc);
  const parts = (doc.verification.sandbox ?? "").split(";").map((p) => p.trim()).filter(Boolean);
  const obs: DemoObservation | undefined = nodes[call]?.obs;
  const total = totalStepMs(steps);
  const head = (
    <>
      <Caption
        numeral={<Numeral value={(total / 1000).toFixed(1)} unit="s" />}
        lines={
          <>
            <p className="text-ink">Sandbox time, {steps.length} checks</p>
            <Line tone={allPass ? "ok" : "fail"}>{allPass ? "All checks passed" : "A check failed"}</Line>
            <Line>Disposable Docker sandbox</Line>
          </>
        }
      />
      <Headline>What ran, and where the time went.</Headline>
    </>
  );
  const body = (
    <>
      <Lead>Cell area on the map is each check&rsquo;s real recorded duration. Command output stays in the local artifact directory and is not part of the public export.</Lead>

      <RailGroup title="Sandbox checks" aside={formatMs(total)}>
        {steps.map((s) => (
          <RailRow key={s.name} testId="gate-step" tone={s.passed ? "ok" : "fail"} label={<span className="mono">{s.name}</span>} sub={s.exit_code ? `exit ${s.exit_code}` : undefined} value={formatMs(s.duration_ms)} selected={step === s.name} onClick={() => setStep(s.name)} />
        ))}
      </RailGroup>
      {parts.length ? (
        <div className="mt-2">
          <Disclosure summary="Isolation the candidate ran under">
            <ul>{parts.map((p) => <li key={p} className="mono py-0.5 text-sm">{p}</li>)}</ul>
          </Disclosure>
        </div>
      ) : null}

      <RailGroup title="Provider calls" aside={ids.verification ? `verification order ${shortId(ids.verification, 8, 4)}` : undefined}>
        {nodes.map((n, i) => (
          <RailRow
            key={n.obs.seq}
            testId="observation-row"
            tone="idle"
            label={<span><span className="text-faint">{n.obs.seq} · </span>{n.obs.method}</span>}
            sub={humanStatus(n.obs.order_status)}
            value={i === 0 ? "start" : `+${formatMicros(n.gapUs)}`}
            selected={call === i}
            onClick={() => setCall(i)}
          />
        ))}
        <p className="mt-2 text-sm leading-5 text-faint">These calls belong to the verification order, not the incident order. Real gaps are shown.</p>
        {obs ? (
          <dl className="mt-2" aria-live="polite" data-testid="call-detail">
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
      </RailGroup>

      <div className="mt-6 flex flex-wrap gap-3 pb-2">
        <OutlineButton onClick={() => go("break")}>Back to Break</OutlineButton>
        <OutlineButton href="/benchmark">Held-out benchmark (preliminary)</OutlineButton>
      </div>
    </>
  );
  return part === "head" ? head : body;
}

export function EvidenceCanvas({ doc, step, setStep }: { doc: DemoExport; step: string; setStep: (s: string) => void }) {
  return <TimeMap steps={doc.verification.steps} selected={step} onSelect={setStep} />;
}
