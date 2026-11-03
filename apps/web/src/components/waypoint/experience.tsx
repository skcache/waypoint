"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, LockKey, WarningCircle } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { firstSentence, receiptModel, ruleTiles, storyFigures, type Column } from "@/lib/evidence";
import { humanStatus, modelLabel, modelNote, money, shortId, titleFor, utcTimestamp } from "@/lib/showcase";
import { ThemeToggle } from "../theme-toggle";
import { Board, counterexampleFacts, stateWord } from "./board";
import { EvidenceDrawer } from "./evidence-drawer";
import { RepairPanel } from "./repair-panel";
import { ReviewPanel } from "./review-panel";
import type { EvidenceSource } from "./types";

type Variant = "showcase" | "demo";

const COLUMNS: Column[] = ["before", "repair", "unsafe"];
const STEPS = ["The incident", "The AI repair", "The check", "Human review"] as const;
const NEXT = ["Next: AI repair", "Next: check", "Next: human review"];

const verdictWord = (v: string) => (v === "VERIFIED" ? "Accepted" : v === "UNSAFE" ? "Rejected" : "Inconclusive");
const PHRASE: Record<Column, string> = { before: "original code", repair: "AI repair", unsafe: "seeded unsafe patch" };
const basename = (p: string) => p.split("/").pop() ?? p;

function Glyph({ state }: { state: "done" | "current" | "todo" }) {
  return (
    <svg width="20" height="20" viewBox="0 0 18 18" aria-hidden className="shrink-0">
      {state === "done" ? (
        <>
          <circle cx="9" cy="9" r="8" fill="currentColor" />
          <path d="M5.4 9.3 8 11.8l4.7-5" fill="none" stroke="var(--c-bg)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : state === "current" ? (
        <>
          <circle cx="9" cy="9" r="7.4" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path d="M9 1.6a7.4 7.4 0 0 1 0 14.8z" fill="currentColor" />
        </>
      ) : (
        <circle cx="9" cy="9" r="7.4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeDasharray="2.4 2.6" />
      )}
    </svg>
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
  return <Instrument doc={doc} source={source} />;
}

function Instrument({ doc, source }: { doc: DemoExport; source: EvidenceSource }) {
  const [step, setStep] = useState(0);
  const [cand, setCand] = useState<Column>("repair");
  const [picked, setPicked] = useState<string | null>(null);
  const [drawer, setDrawer] = useState(false);

  const models = useMemo(() => Object.fromEntries(COLUMNS.map((c) => [c, receiptModel(doc, c)])) as Record<Column, ReturnType<typeof receiptModel>>, [doc]);
  const fig = useMemo(() => storyFigures(doc), [doc]);
  const amount = money(doc.incident.total_minor, doc.incident.currency);
  const boardCol: Column = step === 0 ? "before" : cand;
  const model = models[boardCol];
  const boardEval = boardCol === "before" ? doc.pre_repair : boardCol === "repair" ? doc.oracle : doc.unsafe;
  const tiles = useMemo(() => ruleTiles(boardEval), [boardEval]);
  const tile = tiles.find((t) => t.id === picked);
  const ruleCount = doc.oracle.invariants.length;
  const caseLabel = `Case file ${doc.incident.fault} · ${titleFor(doc)}`;

  function goto(n: number) {
    setStep(n);
    setPicked(null);
    if (n === 2) setCand("repair");
    if (typeof window !== "undefined" && window.innerWidth < 1024) window.scrollTo({ top: 0, behavior: "smooth" });
  }

  const other = boardCol === "unsafe" ? models.repair : boardCol === "repair" ? models.unsafe : null;
  const count = (n: number | null) => (n === null ? "an unknown number of fulfillments" : `${n} fulfillment${n === 1 ? "" : "s"}`);
  const surfaceNote =
    other && model.status && other.status === model.status
      ? `The order status is the same as the ${PHRASE[other.column]}: ${humanStatus(model.status)}. The ledger is not the same. This version has ${count(model.fulfilled)}. The ${PHRASE[other.column]} has ${other.fulfilled ?? "an unknown number"}.`
      : null;
  const provenance =
    boardCol === "before"
      ? "This is the recorded check of the incident order, before the repair."
      : boardCol === "repair"
        ? `The checker verified the patch for the tested scenario only. The evidence is tier ${doc.run.evidence_tier}, a scripted fixture. It makes no claim about live PayPal.`
        : `Historical oracle evidence. ${doc.unsafe.provenance} ${
            doc.unsafe.fresh_trusted_gate.executed
              ? `Fresh trusted gate: ${humanStatus(doc.unsafe.fresh_trusted_gate.result)}. ${doc.unsafe.fresh_trusted_gate.detail ?? ""}`
              : "The fresh trusted gate did not run for this record."
          }`;

  const results = [`${fig.incident.fulfilled ?? "?"} fulfillment${fig.incident.fulfilled === 1 ? "" : "s"}`, `+${fig.repair.added} −${fig.repair.removed}`, `${fig.check.accepted} of ${fig.check.candidates} accepted`, "not approved"];
  const hero = [
    { value: String(fig.incident.fulfilled ?? "?"), unit: "fulfillments", label: `for one payment of ${amount}` },
    { value: `+${fig.repair.added}`, unit: `−${fig.repair.removed} lines`, label: `changed in ${basename(fig.repair.file)} by the AI` },
    {
      value: String(fig.check.accepted),
      unit: `of ${fig.check.candidates} patches accepted`,
      label: `The checker ${verdictWord(models.repair.verdict).toLowerCase()} the AI repair. It ${verdictWord(models.unsafe.verdict).toLowerCase()} the unsafe patch.`,
    },
    { value: String(fig.review.waiting), unit: fig.review.waiting === 1 ? "patch waits for review" : "patches wait for review", label: "No person approved it. Nothing is merged or deployed." },
  ][step];

  return (
    <div className="flex min-h-dvh flex-col lg:h-dvh lg:overflow-hidden">
      <header className="flex h-14 shrink-0 items-center justify-between gap-3 px-4 sm:px-8">
        <div className="flex min-w-0 items-center gap-4">
          <span className="text-[18px] font-semibold">Waypoint</span>
          <span className="truncate text-[14px] text-faint max-sm:hidden" data-testid="case-label">{caseLabel}</span>
        </div>
        <nav aria-label="Primary" className="flex items-center gap-1">
          <button type="button" onClick={() => setDrawer(true)} className="inline-flex min-h-11 items-center rounded-[10px] px-4 text-[15px] font-medium text-muted transition-colors hover:text-ink">
            Evidence
          </button>
          <Link href="/benchmark" className="inline-flex min-h-11 items-center rounded-[10px] px-4 text-[15px] font-medium text-muted transition-colors hover:text-ink max-sm:hidden">
            Benchmark
          </Link>
          <ThemeToggle />
        </nav>
      </header>

      <main className="flex flex-1 flex-col px-4 pb-36 sm:px-8 lg:grid lg:min-h-0 lg:grid-cols-[400px_minmax(0,1fr)] lg:gap-x-10 lg:pb-6">
        <div className="contents lg:flex lg:min-h-0 lg:flex-col">
          <div className="order-1 shrink-0 pt-2 lg:order-none lg:pt-4">
            <p className="max-w-[44ch] text-[13px] leading-[1.45] text-muted">
              Waypoint uses an AI to repair a payment bug. A separate checker tests each repair against {ruleCount} rules. This run is {source === "recorded" ? "recorded" : "local"}.
            </p>
            <div key={step} className="enter mt-5" data-testid="hero">
              <div className="flex items-baseline gap-3">
                <span className="numeral text-[clamp(5.5rem,8vw,7.4rem)]" data-testid="hero-value">{hero.value}</span>
                <span className="text-[20px] font-medium text-muted">{hero.unit}</span>
              </div>
              <p className="mt-3 max-w-[34ch] text-[14px] leading-[1.45]">{hero.label}</p>
            </div>
          </div>

          <nav aria-label="Walkthrough" className="order-3 mt-6 min-h-0 lg:order-none lg:flex-1 lg:overflow-y-auto lg:pb-6 lg:pr-2 lg:[mask-image:linear-gradient(to_bottom,black_calc(100%-24px),transparent)]">
            <p className="text-[13px] font-medium text-faint" data-testid="step-count">Step {step + 1} of {STEPS.length}</p>
            <ol className="mt-2 border-t border-line">
              {STEPS.map((title, i) => {
                const state = i < step ? "done" : i === step ? "current" : "todo";
                const active = i === step;
                return (
                  <li key={title} className="border-b border-line">
                    <button
                      type="button"
                      onClick={() => goto(i)}
                      aria-current={active ? "step" : undefined}
                      data-step-row={i + 1}
                      className={`flex min-h-12 w-full items-center gap-3 text-left transition-colors ${active ? "text-ink" : "text-muted hover:text-ink"}`}
                    >
                      <Glyph state={state} />
                      <span className="flex-1 text-[15px] font-medium">{title}</span>
                      <span className="text-[14px] tabular-nums text-faint">{results[i]}</span>
                    </button>
                    {active ? (
                      <div className="enter pb-5 pl-8">
                        <StepBody step={i} doc={doc} amount={amount} models={models} cand={cand} setCand={(c) => { setCand(c); setPicked(null); }} fig={fig} />
                      </div>
                    ) : null}
                  </li>
                );
              })}
            </ol>
          </nav>

          <div className="fixed inset-x-0 bottom-0 z-40 border-t border-line bg-background px-4 pb-3 pt-3 sm:px-8 lg:static lg:order-none lg:shrink-0 lg:border-0 lg:bg-transparent lg:px-0 lg:pb-0 lg:pt-5">
            <div data-testid="human-gate" className="flex items-center gap-2 text-[13px] leading-snug text-muted">
              <LockKey size={16} className="shrink-0" aria-hidden />
              <span className="min-w-0 flex-1">{fig.review.waiting ? "No person approved the patch. Nothing is merged or deployed." : "The evidence records the review state."}</span>
              {step !== 3 ? <button type="button" onClick={() => goto(3)} className="inline-flex min-h-11 shrink-0 items-center font-medium text-ink underline underline-offset-4">Details</button> : null}
            </div>
            <div className="mt-2 flex gap-2">
              <button
                type="button"
                onClick={() => goto(step - 1)}
                disabled={step === 0}
                aria-label="Back"
                className="grid size-12 shrink-0 place-items-center rounded-[12px] border border-line-strong transition-colors hover:bg-sunk disabled:opacity-35 disabled:hover:bg-transparent"
              >
                <ArrowLeft size={18} aria-hidden />
              </button>
              {step < 3 ? (
                <button
                  type="button"
                  onClick={() => goto(step + 1)}
                  data-testid="next"
                  className="inline-flex h-12 flex-1 items-center justify-between rounded-[12px] bg-accent pl-6 pr-5 text-[16px] font-medium text-on-accent transition-[opacity,transform] hover:opacity-90 active:scale-[0.98]"
                >
                  {NEXT[step]}
                  <ArrowRight size={18} aria-hidden />
                </button>
              ) : (
                <button type="button" onClick={() => setDrawer(true)} className="inline-flex h-12 flex-1 items-center justify-between rounded-[12px] bg-accent pl-6 pr-5 text-[16px] font-medium text-on-accent transition-[opacity,transform] hover:opacity-90 active:scale-[0.98]">
                  Open evidence
                  <ArrowRight size={18} aria-hidden />
                </button>
              )}
            </div>
          </div>
        </div>

        <section aria-label="Stage" className="order-2 mt-8 flex min-h-0 flex-col lg:order-none lg:mt-0 lg:pt-1">
          {step === 1 ? (
            <div key="repair" className="enter flex min-h-0 flex-1 flex-col">
              <RepairPanel doc={doc} />
            </div>
          ) : step === 3 ? (
            <div key="review" className="enter flex min-h-0 flex-1 flex-col">
              <ReviewPanel doc={doc} source={source} onEvidence={() => setDrawer(true)} />
            </div>
          ) : (
            <div key="board" className="flex min-h-0 flex-1 flex-col">
              <div className="flex flex-wrap items-end justify-between gap-x-10 gap-y-3 pb-4" data-testid="candidate-facts" data-candidate={boardCol}>
                <div>
                  <p className="text-[20px] font-medium">{model.label}</p>
                  <p className="text-[13px] text-faint">
                    {model.orderRole}
                    {model.orderId ? <> · <span className="mono">{shortId(model.orderId, 8, 4)}</span></> : null}
                  </p>
                </div>
                <dl className="flex flex-wrap gap-x-9 gap-y-2">
                  <div>
                    <dt className="text-[13px] text-faint">Order status</dt>
                    <dd className="text-[16px] font-medium" data-testid="fact-status">{humanStatus(model.status)}</dd>
                  </div>
                  <div>
                    <dt className="text-[13px] text-faint">Captured</dt>
                    <dd className="text-[16px] font-medium" data-testid="fact-captured">{model.captured}</dd>
                  </div>
                  <div>
                    <dt className="text-[13px] text-faint">Fulfilled</dt>
                    <dd className={`text-[16px] font-medium ${model.balanced ? "" : "text-bad"}`} data-testid="fact-fulfilled">{model.fulfilled ?? "?"}</dd>
                  </div>
                  <div>
                    <dt className="text-[13px] text-faint">Checker</dt>
                    <dd className="text-[16px] font-medium" data-testid="fact-verdict">{verdictWord(model.verdict)}</dd>
                  </div>
                </dl>
              </div>
              <div className="h-[64svh] min-h-[432px] lg:h-auto lg:min-h-0 lg:flex-1">
                <Board tiles={tiles} lit={model.verdict === "VERIFIED"} selected={picked} onSelect={setPicked} label={`${model.label}: ${tiles.length} rules`} />
              </div>
              <div className="mt-4 grid gap-x-10 gap-y-3 lg:min-h-[96px] lg:grid-cols-[1.35fr_1fr]">
                <div className="text-[15px] leading-[1.5]" aria-live="polite" data-testid="inspector">
                  {tile ? (
                    <>
                      <p>
                        <b className="font-medium">{tile.id} · {stateWord(tile.status)}.</b> {tile.title}
                      </p>
                      {counterexampleFacts(tile.counterexample).length ? (
                        <p className="mt-1 text-muted">{counterexampleFacts(tile.counterexample).join(" · ")}</p>
                      ) : tile.status === "FAIL" ? (
                        <p className="mt-1 text-muted">The checker exported no counterexample fields for this rule.</p>
                      ) : null}
                    </>
                  ) : step === 2 && surfaceNote ? (
                    <p data-testid="surface-note">{surfaceNote}</p>
                  ) : (
                    <p className="text-muted">Each tile is one rule. A failed rule has a large tile. A dotted tile does not apply. Select a tile to read it.</p>
                  )}
                </div>
                <p className="text-[13px] leading-[1.55] text-faint" data-testid="provenance">{provenance}</p>
              </div>
            </div>
          )}
        </section>
      </main>

      <p className="sr-only">
        Run <span data-testid="demo-run-id">{doc.run.run_id}</span>
      </p>
      <EvidenceDrawer doc={doc} source={source} open={drawer} onClose={() => setDrawer(false)} />
    </div>
  );
}

function StepBody({
  step,
  doc,
  amount,
  models,
  cand,
  setCand,
  fig,
}: {
  step: number;
  doc: DemoExport;
  amount: string;
  models: Record<Column, ReturnType<typeof receiptModel>>;
  cand: Column;
  setCand: (c: Column) => void;
  fig: ReturnType<typeof storyFigures>;
}) {
  const d = doc.diagnosis;
  const seconds = (ms?: number) => (ms ? `${(ms / 1000).toFixed(1)} s` : "–");
  const p = "text-[14px] leading-[1.6] text-muted";

  if (step === 0) {
    return (
      <div className="space-y-3">
        <p className={p}>
          A customer paid {amount} one time. The ledger shows {fig.incident.fulfilled ?? "more than 1"} fulfillments for this payment.
          {fig.incident.statusFlagged ? ` The order status is ${humanStatus(models.before.status)}. The checker flags this (rule I8).` : ""}
        </p>
        <p className={p}>Each tile on the board is one rule. A failed rule has a large tile.</p>
        {fig.incident.failed.length ? <p className="text-[14px] font-medium">Failed rules: {fig.incident.failed.join(", ")}</p> : null}
      </div>
    );
  }
  if (step === 1) {
    return (
      <div className="space-y-4">
        <p className={p}>The AI read the incident data. It found a cause and wrote the patch. The patch ran only in a sandbox. The cause is a hypothesis. The next step tests the patch.</p>
        {d ? (
          <>
            <div>
              <p className="text-[13px] font-medium text-faint">Cause found by the AI</p>
              <p className="mt-1 text-[15px] leading-[1.5]">{firstSentence(d.root_cause, 300)}</p>
            </div>
            {d.uncertainties.length ? (
              <div>
                <p className="text-[13px] font-medium text-faint">What the AI cannot know</p>
                <p className="mt-1 text-[14px] leading-[1.55] text-muted">{d.uncertainties[0]}</p>
              </div>
            ) : null}
            <details className="group border-t border-line">
              <summary className="flex min-h-11 items-center justify-between text-[14px] font-medium">
                Full diagnosis
                <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
              </summary>
              <div className={`space-y-3 pb-3 ${p}`}>
                <p><b className="font-medium text-ink">Cause.</b> {d.root_cause}</p>
                <p><b className="font-medium text-ink">Mechanism.</b> {d.mechanism}</p>
                <p><b className="font-medium text-ink">Proposed fix.</b> {d.proposed_fix}</p>
                {d.uncertainties.length > 1 ? <ul className="list-disc space-y-2 pl-5">{d.uncertainties.slice(1).map((u) => <li key={u}>{u}</li>)}</ul> : null}
                {d.supporting_evidence.length ? <p><b className="font-medium text-ink">Evidence.</b> <span className="mono">{d.supporting_evidence.join(" · ")}</span></p> : null}
                <p><b className="font-medium text-ink">Files.</b> <span className="mono break-all">{d.suspected_files.join(", ")}</span></p>
              </div>
            </details>
          </>
        ) : (
          <p className={p}>This run has no diagnosis data.</p>
        )}
        <div data-testid="model-strip" className="border-t border-line pt-3 text-[13px] leading-[1.55] text-faint">
          <p>
            <b className="font-medium text-muted">{modelLabel(doc)}</b> · <span className="mono">{doc.run.model}</span>
          </p>
          <p className="mt-1">
            {doc.run.model_calls.map((c) => `${c.purpose} ${seconds(c.latency_ms)}`).join(", ")} · {doc.run.prompt_tokens.toLocaleString("en-US")} tokens in,{" "}
            {doc.run.completion_tokens.toLocaleString("en-US")} out · {utcTimestamp(doc.run.model_recorded_at ?? doc.run.finished_at)}
          </p>
          <p className="mt-1">{modelNote(doc)} The patch applies only to a disposable worktree. Base: {shortId(doc.run.base_revision, 10, 0)}.</p>
        </div>
      </div>
    );
  }
  if (step === 2) {
    return (
      <div className="space-y-4">
        <p className={p}>A separate program tests each version of the code. It reads the ledger after each test. Select a version.</p>
        <ul className="border-t border-line" aria-label="Versions of the code">
          {COLUMNS.map((c) => {
            const mm = models[c];
            return (
              <li key={c} className="border-b border-line">
                <button
                  type="button"
                  data-testid={`candidate-${c}`}
                  aria-pressed={cand === c}
                  onClick={() => setCand(c)}
                  className={`flex min-h-[52px] w-full items-center gap-3 py-2 text-left transition-colors ${cand === c ? "text-ink" : "text-muted hover:text-ink"}`}
                >
                  <span className="flex-1">
                    <span className="block text-[15px] font-medium">{mm.label}</span>
                    <span className="block text-[13px] text-faint">{verdictWord(mm.verdict)}{mm.failed.length ? ` · fails ${mm.failed.join(", ")}` : " · no failed rule"}</span>
                  </span>
                  <span className="text-[14px] tabular-nums text-faint">{mm.fulfilled ?? "?"} fulfillment{mm.fulfilled === 1 ? "" : "s"}</span>
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    );
  }
  return <p className={p}>The AI does not merge code. A person must approve the patch. This page cannot merge, deploy, or change a payment.</p>;
}

function Skeleton() {
  return (
    <main className="grid min-h-dvh place-items-center px-5" aria-busy="true" aria-live="polite">
      <div>
        <p className="numeral text-[clamp(4rem,12vw,9rem)]">Waypoint</p>
        <p className="mt-6 text-[16px] text-muted">Loading evidence</p>
      </div>
    </main>
  );
}

function EvidenceError({ message }: { message: string }) {
  return (
    <main className="mx-auto max-w-[640px] px-5 py-24">
      <WarningCircle size={28} weight="bold" className="text-bad" aria-hidden />
      <h1 className="mt-4 text-4xl font-medium tracking-[-0.02em]">Evidence unavailable</h1>
      <p className="mt-3 text-base leading-[1.6] text-muted">
        The page needs the evidence file <span className="mono text-ink">public/demo/recorded.json</span>. Run{" "}
        <span className="mono text-ink">make demo</span> to make a new local replay.
      </p>
      {message ? <p className="mono mt-3 text-sm text-faint">{message}</p> : null}
    </main>
  );
}
