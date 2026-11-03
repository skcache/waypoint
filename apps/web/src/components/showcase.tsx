"use client";

import Link from "next/link";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  ArrowRight,
  ArrowUpRight,
  CaretDown,
  Check,
  Copy,
  Fingerprint,
  TerminalWindow,
  WarningCircle,
} from "@phosphor-icons/react";

import type {
  DemoEval,
  DemoExport,
  DemoInvariant,
  DemoObservation,
} from "@/lib/demo";
import {
  captureObservation,
  failedInvariants,
  faultTitle,
  humanStatus,
  modelLabel,
  modelNote,
  money,
  observationTierLabel,
  runKindLabel,
  sourceLabel,
  tierNote,
  originalOrder,
  shortId,
  shortTime,
  stageMeta,
  STAGES,
  titleFor,
  utcTimestamp,
  verificationOrder,
  type CandidateMode,
  type StageId,
} from "@/lib/showcase";

type Variant = "showcase" | "demo";

type ShowcaseProps = {
  initial: DemoExport | null;
  variant: Variant;
};

type DiffLine = {
  kind: "meta" | "hunk" | "add" | "del" | "context";
  text: string;
  oldNo?: number;
  newNo?: number;
};

function stageCopy(doc: DemoExport, stage: StageId): { title: string; description: string } {
  const failed = failedInvariants(doc.pre_repair.invariants);
  switch (stage) {
    case "incident": {
      if (captureObservation(doc)?.capture_status === "COMPLETED") {
        const duplicate = failed.some((item) => item.id === "I13");
        return {
          title: "The verified capture is not the final order state.",
          description: duplicate
            ? `The original incident order ends ${humanStatus(doc.incident.order_status)}. The pre-repair oracle records a verified capture and duplicate fulfillment effects for that payment.`
            : `The original incident order ends ${humanStatus(doc.incident.order_status)}. The pre-repair oracle records a verified capture for that payment.`,
        };
      }
      return {
        title: "The pre-repair ledger fails.",
        description: `The pre-repair oracle flags ${failed.length} invariant${failed.length === 1 ? "" : "s"} for this ${faultTitle(doc.incident.fault).toLowerCase()} case.`,
      };
    }
    case "diagnosis":
      return {
        title: "The model proposes a cause.",
        description: `${modelLabel(doc)} output explains the failure, names the source path, and proposes the scoped repair. This is a hypothesis, not the oracle verdict.`,
      };
    case "patch": {
      const webhook = doc.patch.changed_files.some((file) => file.endsWith("webhook.go"));
      const count = doc.patch.changed_files.length;
      return {
        title: webhook
          ? "The candidate changes the webhook handler."
          : `The candidate changes ${count} file${count === 1 ? "" : "s"}.`,
        description:
          "The generated patch is applied inside a disposable worktree. The diff below is the candidate output, not a merged change.",
      };
    }
    case "verification":
      return {
        title: "Build and tests run in a disposable sandbox.",
        description:
          "Build, trusted tests, fixture execution, and the independent judge run against the candidate in a disposable Docker environment.",
      };
    case "oracle":
      return {
        title: "A separate judge evaluates the ledger invariants.",
        description:
          "A separate judge re-reads the durable ledger and evaluates the financial invariants for the tested scenario.",
      };
    case "review":
      return {
        title: "The candidate stops at human review.",
        description:
          "The repair reaches human review only as evidence. No patch is merged, deployed, or allowed to move money.",
      };
  }
}

function Showcase({ initial, variant }: ShowcaseProps) {
  const [doc, setDoc] = useState<DemoExport | null>(initial);
  const [source, setSource] = useState<"recorded" | "local">("recorded");
  const [loading, setLoading] = useState(!initial);
  const [error, setError] = useState("");
  const [stage, setStage] = useState<StageId>("incident");
  const [candidate, setCandidate] = useState<CandidateMode>("verified");

  useEffect(() => {
    let live = true;
    async function load() {
      if (initial) {
        setDoc(initial);
        setLoading(false);
      }
      const forceRecorded =
        typeof window !== "undefined" &&
        new URLSearchParams(window.location.search).get("source") === "recorded";
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
          if (live) {
            setError(loadError instanceof Error ? loadError.message : "Could not load evidence.");
          }
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

  if (loading && !doc) {
    return <EvidenceSkeleton />;
  }
  if (!doc) {
    return <EvidenceError message={error} />;
  }

  return (
    <div className="pb-20">
      {variant === "showcase" ? (
        <Hero doc={doc} source={source} />
      ) : (
        <DemoIntro doc={doc} source={source} />
      )}
      <CaseFile
        doc={doc}
        source={source}
        stage={stage}
        setStage={setStage}
        candidate={candidate}
        setCandidate={setCandidate}
      />
    </div>
  );
}

function Hero({ doc, source }: { doc: DemoExport; source: "recorded" | "local" }) {
  return (
    <section className="border-b border-line">
      <div className="grid gap-12 py-12 lg:grid-cols-[1.08fr_.92fr] lg:items-end lg:py-16">
        <div className="max-w-[780px]">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 font-mono text-[10px] uppercase tracking-[0.16em] text-faint">
            <span>Payment repair system</span>
            <span className="h-px w-8 bg-line-strong" />
            <span>{sourceLabel(doc, source)}</span>
            <span className="border border-line px-1.5 py-0.5 text-[10px] tracking-[0.1em] text-muted">
              tier {doc.run.evidence_tier}
            </span>
            <span className="normal-case tracking-normal">no live payments</span>
          </div>
          <h1 className="mt-7 max-w-[760px] text-balance text-[clamp(2.6rem,6.4vw,6.7rem)] font-semibold leading-[0.92] tracking-[-0.065em]">
            Repair the code.
            <br />
            <span className="text-accent">Prove the money.</span>
          </h1>
          <p className="mt-7 max-w-[52ch] text-[15px] leading-6 text-muted">
            An AI engineer investigates a failed PayPal integration, proposes a source
            repair, and an independent oracle checks the financial behavior before human
            review.
          </p>
          <div className="mt-8 flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:gap-x-6">
            <a
              href="#case-file"
              className="group inline-flex w-full items-center justify-center gap-2 border border-ink bg-ink px-4 py-2.5 text-xs font-medium text-background transition-colors hover:border-accent hover:bg-accent sm:w-auto"
            >
              Open the case file
              <ArrowRight size={14} weight="bold" className="transition-transform group-hover:translate-x-0.5" />
            </a>
            <Link
              href="/benchmark"
              className="group inline-flex items-center gap-1.5 text-xs font-medium text-muted underline decoration-line-strong underline-offset-4 transition-colors hover:text-ink"
            >
              Evidence and limits
              <ArrowUpRight size={13} weight="bold" />
            </Link>
          </div>
        </div>
        <CaseTeaser doc={doc} />
      </div>
    </section>
  );
}

function DemoIntro({ doc, source }: { doc: DemoExport; source: "recorded" | "local" }) {
  return (
    <section className="border-b border-line py-8 sm:py-10">
      <div className="flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.16em] text-faint">
            {sourceLabel(doc, source)}
          </p>
          <h1 className="mt-3 text-3xl font-semibold tracking-[-0.045em] sm:text-4xl">
            {titleFor(doc)}
          </h1>
          <p className="mt-2 max-w-[60ch] text-sm leading-6 text-muted">
            The full case file is below. Switch stages to inspect the failure, the model
            repair, the sandbox, and the human boundary.
          </p>
        </div>
        <span className="border border-line px-2 py-1 font-mono text-[10px] uppercase tracking-[0.12em] text-muted">
          tier {doc.run.evidence_tier} · {runKindLabel(doc.run.kind)}
        </span>
      </div>
    </section>
  );
}

function CaseTeaser({ doc }: { doc: DemoExport }) {
  const before = doc.pre_repair.verdict;
  const after = doc.oracle.verdict;
  const verified = after === "VERIFIED_FOR_TESTED_SCENARIOS";
  return (
    <aside className="border border-line bg-surface p-5 sm:p-6">
      <div className="flex items-start justify-between gap-4 border-b border-line pb-4">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-faint">Case {doc.incident.fault}</p>
          <p className="mt-1 text-lg font-semibold tracking-tight">{titleFor(doc)}</p>
        </div>
        <span className="font-mono text-[10px] text-faint">{runKindLabel(doc.run.kind)}</span>
      </div>
      <div className="grid grid-cols-[1fr_auto] items-end gap-5 border-b border-line py-5">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Original order</p>
          <p className="mt-1.5 break-all font-mono text-[11px] text-ink">{shortId(originalOrder(doc), 10, 6)}</p>
        </div>
        <div className="text-right">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Amount</p>
          <p className="mt-1 whitespace-nowrap font-mono text-xl font-medium tracking-tight text-ink">
            {money(doc.incident.total_minor, doc.incident.currency)}
          </p>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-px bg-line">
        <div className="bg-surface py-4 pr-4">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Before repair</p>
          <p className="mt-1 text-sm font-medium text-bad">{before}</p>
          <p className="mt-0.5 font-mono text-[10px] text-faint">order status {humanStatus(doc.pre_repair.order_status)}</p>
        </div>
        <div className="bg-surface py-4 pl-4">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">After oracle</p>
          <p className={`mt-1 text-sm font-medium ${verified ? "text-ok" : "text-bad"}`}>
            {verified ? "VERIFIED" : after}
          </p>
          <p className="mt-0.5 font-mono text-[10px] text-faint">order status {humanStatus(doc.oracle.order_status)}</p>
        </div>
      </div>
      <div className="hidden pt-4 sm:block">
        <div className="flex items-center justify-between gap-3">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Investigation path</p>
          <p className="font-mono text-[10px] text-faint">6 stages</p>
        </div>
        <div className="mt-3 grid grid-cols-3 gap-x-2 gap-y-2">
          {STAGES.map((item, index) => (
            <div key={item.id} className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
              <span className="text-faint">{String(index + 1).padStart(2, "0")}</span>
              <span>{item.short}</span>
            </div>
          ))}
        </div>
      </div>
      <p className="mt-4 border-t border-line pt-3 font-mono text-[9px] leading-4 text-faint">
        {tierNote(doc)}
      </p>
    </aside>
  );
}

function CaseFile({
  doc,
  source,
  stage,
  setStage,
  candidate,
  setCandidate,
}: {
  doc: DemoExport;
  source: "recorded" | "local";
  stage: StageId;
  setStage: (stage: StageId) => void;
  candidate: CandidateMode;
  setCandidate: (mode: CandidateMode) => void;
}) {
  const failed = failedInvariants(doc.pre_repair.invariants);
  const copy = stageCopy(doc, stage);
  return (
    <section id="case-file" className="scroll-mt-20 pt-10 sm:pt-12">
      <CaseHeader doc={doc} source={source} />
      <StageRail doc={doc} stage={stage} candidate={candidate} setStage={setStage} />
      <div id="stage-panel" role="tabpanel" aria-labelledby={`stage-tab-${stage}`} className="stage-enter">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-line py-7 sm:py-8">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-faint">
              {STAGES.find((item) => item.id === stage)?.label}
            </p>
            <h2 className="mt-2 max-w-[780px] text-balance text-2xl font-semibold tracking-[-0.035em] sm:text-3xl">
              {copy.title}
            </h2>
            <p className="mt-2 max-w-[68ch] text-[13px] leading-6 text-muted">
              {copy.description}
            </p>
          </div>
          <p className="font-mono text-[10px] text-faint">{stageMeta(doc, stage)}</p>
        </div>
        <div className="py-8 sm:py-10">
          {stage === "incident" && <IncidentStage doc={doc} failed={failed} />}
          {stage === "diagnosis" && <DiagnosisStage doc={doc} />}
          {stage === "patch" && <PatchStage doc={doc} />}
          {stage === "verification" && <ExecutionStage doc={doc} />}
          {stage === "oracle" && (
            <OracleStage key={candidate} doc={doc} candidate={candidate} setCandidate={setCandidate} />
          )}
          {stage === "review" && (
            <ReviewStage doc={doc} onCompare={() => { setStage("oracle"); setCandidate("unsafe"); }} />
          )}
        </div>
      </div>
    </section>
  );
}

function CaseHeader({ doc, source }: { doc: DemoExport; source: "recorded" | "local" }) {
  const original = originalOrder(doc);
  const verification = verificationOrder(doc);
  return (
    <div className="flex flex-col justify-between gap-6 border-y border-line py-5 sm:flex-row sm:items-end">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[10px] text-faint">
          <span>{doc.incident.id}</span>
          <span className="text-line-strong">/</span>
          <span>{doc.incident.fault}</span>
          <span className="text-line-strong">/</span>
          <span>{sourceLabel(doc, source)}</span>
        </div>
        <h2 className="mt-2 text-2xl font-semibold tracking-[-0.035em] sm:text-[28px]">
          {titleFor(doc)}
        </h2>
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[10px] text-faint">
          <span>Original order {shortId(original, 8, 4)}</span>
          <span>Verification order {shortId(verification, 8, 4)}</span>
        </div>
      </div>
      <div className="flex shrink-0 items-end gap-6 sm:text-right">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Run</p>
          <p className="mt-1 break-all font-mono text-[10px] text-muted" data-testid="demo-run-id">
            {doc.run.run_id}
          </p>
        </div>
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Amount</p>
          <p className="mt-1 whitespace-nowrap font-mono text-sm text-ink">
            {money(doc.incident.total_minor, doc.incident.currency)}
          </p>
        </div>
      </div>
    </div>
  );
}

function StageRail({
  doc,
  stage,
  candidate,
  setStage,
}: {
  doc: DemoExport;
  stage: StageId;
  candidate: CandidateMode;
  setStage: (stage: StageId) => void;
}) {
  function move(next: number) {
    const bounded = (next + STAGES.length) % STAGES.length;
    const nextStage = STAGES[bounded].id;
    setStage(nextStage);
    requestAnimationFrame(() => {
      document.getElementById(`stage-tab-${nextStage}`)?.focus();
    });
  }

  return (
    <div
      role="tablist"
      aria-label="Investigation stages"
      className="grid grid-cols-3 border-b border-line sm:grid-cols-6"
    >
      {STAGES.map((item, index) => {
        const active = item.id === stage;
        return (
          <button
            key={item.id}
            id={`stage-tab-${item.id}`}
            role="tab"
            type="button"
            aria-selected={active}
            aria-controls="stage-panel"
            data-stage={item.id}
            tabIndex={active ? 0 : -1}
            onClick={() => setStage(item.id)}
            onKeyDown={(event) => {
              if (event.key === "ArrowRight") {
                event.preventDefault();
                move(index + 1);
              }
              if (event.key === "ArrowLeft") {
                event.preventDefault();
                move(index - 1);
              }
            }}
            className={`relative flex min-w-0 flex-col items-start gap-1.5 border-b border-r border-line px-3 py-3.5 text-left last:border-r-0 sm:border-b-0 sm:px-4 ${
              active ? "bg-surface" : "bg-transparent hover:bg-surface/60"
            }`}
          >
            {active ? <span className="absolute inset-x-0 top-0 h-0.5 bg-accent" /> : null}
            <span className="font-mono text-[10px] text-faint">{String(index + 1).padStart(2, "0")}</span>
            <span className={`text-[13px] ${active ? "font-medium text-ink" : "text-muted"}`}>{item.label}</span>
            <span className="max-w-full truncate font-mono text-[10px] text-faint">{stageMeta(doc, item.id, candidate)}</span>
          </button>
        );
      })}
    </div>
  );
}

function IncidentStage({ doc, failed }: { doc: DemoExport; failed: DemoInvariant[] }) {
  const observations = doc.incident.observations ?? [];
  const [selected, setSelected] = useState(() => {
    const captureIndex = observations.findIndex((item) => Boolean(item.capture_id));
    return captureIndex >= 0 ? captureIndex : 0;
  });
  const observation = observations[selected] ?? observations[0];
  const original = originalOrder(doc);
  const verification = verificationOrder(doc);
  const verificationStatus = observations[observations.length - 1]?.order_status;

  return (
    <div className="grid gap-8 lg:grid-cols-[1.1fr_.9fr]" data-testid="demo-incident">
      <div>
        <div className="grid gap-px border-y border-line bg-line sm:grid-cols-2">
          <div className="bg-background px-4 py-4">
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Original incident order</p>
            <p className="mt-2 break-all font-mono text-[12px] text-ink">{original}</p>
            <p className="mt-1 text-[11px] text-muted">Pre-repair status {humanStatus(doc.incident.order_status)}</p>
          </div>
          <div className="bg-background px-4 py-4">
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Verification order</p>
            <p className="mt-2 break-all font-mono text-[12px] text-ink">{verification ?? "not exported"}</p>
            <p className="mt-1 text-[11px] text-muted">
              Created by the candidate fixture{verificationStatus ? ` · ${humanStatus(verificationStatus)}` : ""}
            </p>
          </div>
        </div>

        <div className="mt-7 border-y border-line bg-surface px-4 py-4">
          <div className="flex items-baseline justify-between gap-3">
            <h3 className="text-sm font-medium tracking-tight">What the pre-repair oracle found</h3>
            <span className="font-mono text-[11px] uppercase tracking-[0.08em] text-bad">{humanStatus(doc.pre_repair.verdict)}</span>
          </div>
          <p className="mt-2 max-w-[70ch] text-[13px] leading-6 text-muted">{doc.pre_repair.reason}</p>
          {failed.length ? (
            <ul className="mt-3 space-y-2 border-t border-line/70 pt-3">
              {failed.map((item) => (
                <li key={item.id} className="flex gap-2 text-[12px] leading-5 text-muted">
                  <span className="font-mono text-bad">{item.id}</span>
                  <span>{item.summary}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>

        <LedgerCounterexamples items={failed} />

        <div className="mt-7 border-l-2 border-accent bg-surface px-3 py-2.5">
          <p className="flex items-start gap-2 text-[11px] leading-5 text-ink">
            <Fingerprint size={14} weight="bold" className="mt-0.5 shrink-0 text-accent" aria-hidden />
            Provider calls belong to the verification order, not the original incident.
          </p>
        </div>

        <div className="mt-7">
          <div className="flex items-baseline justify-between gap-3 border-b border-line pb-3">
            <h3 className="text-sm font-medium tracking-tight">Provider calls</h3>
            <span className="font-mono text-[10px] text-faint">
              {observations.length} recorded · tier {observationTierLabel(doc)}
            </span>
          </div>
          <div className="grid grid-cols-[38px_1fr_auto] gap-3 border-b border-line px-3 py-2 font-mono text-[9px] uppercase tracking-[0.1em] text-faint">
            <span>Seq</span>
            <span>Call</span>
            <span>UTC</span>
          </div>
          <div>
            {observations.map((item, index) => {
              const active = index === selected;
              const flags = [
                item.mutation_applied ? "mutation applied" : null,
                item.idempotent_replay ? "idempotent replay" : null,
                item.response_dropped ? "response lost" : null,
                item.definite_refusal ? "definite refusal" : null,
              ].filter(Boolean);
              return (
                <button
                  key={`${item.seq}-${item.method}`}
                  type="button"
                  onClick={() => setSelected(index)}
                  aria-pressed={active}
                  data-testid="observation-row"
                  className={`grid w-full grid-cols-[38px_1fr_auto] items-start gap-3 border-b border-line/80 border-l-2 py-3 pl-3 pr-1 text-left transition-colors ${
                    active ? "border-l-ink bg-surface" : "border-l-transparent hover:bg-surface/60"
                  }`}
                >
                  <span className={`font-mono text-[11px] ${active ? "text-ink" : "text-faint"}`}>
                    {String(item.seq).padStart(2, "0")}
                  </span>
                  <span className="min-w-0">
                    <span className="font-mono text-[11px] uppercase tracking-[0.06em] text-ink">{item.method}</span>
                    <span className="ml-2 text-[11px] text-muted">{humanStatus(item.order_status)}</span>
                    {flags.length ? (
                      <span className="mt-1 flex flex-wrap gap-x-2 font-mono text-[10px] text-faint">
                        {flags.map((flag) => <span key={flag}>{flag}</span>)}
                      </span>
                    ) : null}
                  </span>
                  <span className="font-mono text-[10px] text-faint">{shortTime(item.at)}</span>
                </button>
              );
            })}
          </div>
        </div>
        {!observations.some((item) => /webhook/i.test(item.method)) ? (
          <p className="mt-3 text-[10px] leading-4 text-faint">
            This artifact contains {observations.length} provider calls and no webhook delivery rows.
            Any webhook explanation in Diagnosis is a model hypothesis, not an observed row.
          </p>
        ) : null}

      </div>

      <EventDetail observation={observation} />
    </div>
  );
}

function LedgerCounterexamples({ items }: { items: DemoInvariant[] }) {
  const evidenced = items.filter((item) => item.counterexample);
  if (!evidenced.length) return null;
  return (
    <section className="mt-6 border border-line bg-surface px-4 py-4" data-testid="ledger-counterexamples">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-sm font-medium tracking-tight">Pre-repair ledger counterexamples</h3>
        <span className="font-mono text-[10px] text-faint">oracle evidence</span>
      </div>
      <p className="mt-2 max-w-[70ch] text-[11px] leading-5 text-muted">
        These are the oracle&apos;s recorded counterexamples. They are separate from the provider
        call sequence.
      </p>
      <ul className="mt-3 divide-y divide-line/70 border-t border-line">
        {evidenced.map((item) => {
          const c = item.counterexample;
          if (!c) return null;
          return (
            <li key={item.id} className="py-3">
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-[10px] text-bad">{item.id}</span>
                <span className="text-[11px] leading-5 text-ink">{item.summary}</span>
              </div>
              {c.order_id ? (
                <p className="mt-1 pl-8 font-mono text-[10px] text-faint">order {shortId(c.order_id, 8, 4)}</p>
              ) : null}
              {item.id === "I13" && c.effect_count ? (
                <div className="mt-1.5 pl-8">
                  <p className="font-mono text-[11px] text-bad">{c.effect_count} fulfillment effects</p>
                  {c.effects?.length ? (
                    <ul className="mt-1 flex flex-wrap gap-x-4 gap-y-1 font-mono text-[10px] text-muted">
                      {c.effects.map((effect) => <li key={effect}>{effect}</li>)}
                    </ul>
                  ) : null}
                </div>
              ) : null}
              {item.id === "I8" && c.final_status ? (
                <p className="mt-1.5 pl-8 font-mono text-[10px] leading-5 text-muted">
                  final status {humanStatus(c.final_status)}
                  {c.confirm_event_id ? ` · journal confirmation event ${c.confirm_event_id}` : ""}
                </p>
              ) : null}
              {item.id === "I14" && c.event_id ? (
                <p className="mt-1.5 pl-8 font-mono text-[10px] leading-5 text-muted">
                  ledger fulfillment event {c.event_id}
                  {c.final_status ? ` · order status ${humanStatus(c.final_status)}` : ""}
                </p>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function EventDetail({ observation }: { observation?: DemoObservation }) {
  if (!observation) {
    return (
      <div className="border-t border-line pt-5 text-[12px] leading-6 text-muted">
        No provider observations were exported for this run.
      </div>
    );
  }
  const fields: { label: string; value: ReactNode }[] = [
    { label: "Sequence", value: <span className="font-mono">{String(observation.seq).padStart(2, "0")}</span> },
    { label: "Timestamp", value: <span className="font-mono">{utcTimestamp(observation.at)}</span> },
    { label: "Method", value: <span className="font-mono uppercase">{observation.method}</span> },
    { label: "Provider order ref", value: <span className="font-mono">{observation.order_ref ?? "not recorded"}</span> },
    { label: "Request ID", value: observation.request_id ? <CopyValue value={observation.request_id} /> : <span className="text-faint">not recorded</span> },
    { label: "Durable before call", value: <YesNo value={observation.request_id_durable_before_call} /> },
    { label: "Sent amount", value: observation.sent_amount ? `${observation.sent_amount} ${observation.sent_currency ?? ""}`.trim() : "not recorded" },
    { label: "Mutation applied", value: <YesNo value={observation.mutation_applied} /> },
    { label: "Idempotent replay", value: <YesNo value={observation.idempotent_replay} /> },
    { label: "Response dropped", value: <YesNo value={observation.response_dropped} /> },
    { label: "Definite refusal", value: <YesNo value={observation.definite_refusal} /> },
    { label: "Order status", value: <span className="font-mono">{humanStatus(observation.order_status)}</span> },
    { label: "Capture ID", value: observation.capture_id ? <CopyValue value={observation.capture_id} /> : <span className="text-faint">not recorded</span> },
    { label: "Capture status", value: <span className="font-mono">{observation.capture_status ?? "not recorded"}</span> },
    { label: "Capture amount", value: observation.capture_amount ? `${observation.capture_amount} ${observation.capture_currency ?? ""}`.trim() : "not recorded" },
    { label: "Stale read", value: <YesNo value={observation.stale_read} /> },
    { label: "Source", value: <span className="font-mono">{observation.source ?? "not recorded"}</span> },
    { label: "Evidence tier", value: <span className="font-mono">{observation.tier ?? "not recorded"}</span> },
  ];
  return (
    <aside className="h-fit border border-line bg-surface lg:sticky lg:top-20">
      <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">
            Selected call · verification order
          </p>
          <p className="mt-1 font-mono text-sm text-ink">
            {String(observation.seq).padStart(2, "0")} / {observation.method}
          </p>
        </div>
        <Fingerprint size={18} weight="bold" className="text-accent" aria-hidden />
      </div>
      <dl className="divide-y divide-line/70 px-4">
        {fields.map((field) => (
          <div key={field.label} className="grid grid-cols-[132px_1fr] gap-3 py-3">
            <dt className="text-[11px] leading-4 text-faint">{field.label}</dt>
            <dd className="min-w-0 break-words text-[12px] leading-4 text-muted">{field.value}</dd>
          </div>
        ))}
      </dl>
      <p className="border-t border-line px-4 py-3 font-mono text-[10px] leading-4 text-faint">
        Raw observation detail, without inferred timestamps or effects.
      </p>
    </aside>
  );
}

function DiagnosisStage({ doc }: { doc: DemoExport }) {
  const diagnosis = doc.diagnosis;
  return (
    <div className="grid gap-10 lg:grid-cols-[1.12fr_.88fr]">
      <div>
        {diagnosis ? (
          <>
            <div className="mb-5 border-l-2 border-accent bg-surface px-3 py-2.5">
              <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-accent">Model hypothesis</p>
              <p className="mt-1 text-[11px] leading-5 text-muted">
                This is the {modelLabel(doc).toLowerCase()}&apos;s explanation, not an independent verdict.
                The oracle in stage 05 is the separate check.
              </p>
            </div>
            <p className="max-w-[780px] text-balance text-xl font-medium leading-[1.35] tracking-[-0.025em] text-ink sm:text-2xl">
              {diagnosis.root_cause}
            </p>
            <div className="mt-8 grid gap-7 border-t border-line pt-6 sm:grid-cols-2">
              <div>
                <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Mechanism</p>
                <p className="mt-2 text-[12px] leading-6 text-muted">{diagnosis.mechanism}</p>
              </div>
              <div>
                <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Proposed repair</p>
                <p className="mt-2 text-[12px] leading-6 text-muted">{diagnosis.proposed_fix}</p>
              </div>
            </div>
            {diagnosis.supporting_evidence?.length ? (
              <div className="mt-8 border-t border-line pt-5">
                <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Evidence the model used</p>
                <ul className="mt-3 space-y-2">
                  {diagnosis.supporting_evidence.map((item) => (
                    <li key={item} className="flex gap-2 text-[11px] leading-5 text-muted">
                      <span className="mt-2 h-px w-3 shrink-0 bg-line-strong" />
                      <span>{item}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {diagnosis.uncertainties?.length ? (
              <details open className="mt-6 border-t border-line pt-4">
                <summary className="flex items-center gap-2 text-[11px] font-medium text-muted">
                  Model uncertainties
                  <CaretDown size={13} weight="bold" />
                </summary>
                <ul className="mt-3 space-y-1.5">
                  {diagnosis.uncertainties.map((item) => (
                    <li key={item} className="text-[11px] leading-5 text-faint">{item}</li>
                  ))}
                </ul>
              </details>
            ) : null}
          </>
        ) : (
          <EmptyState title="No diagnosis exported" body="This run did not produce a structured model diagnosis." />
        )}
      </div>
      <aside className="border-t border-line pt-5 lg:border-l lg:border-t-0 lg:pl-7 lg:pt-0">
        <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{modelLabel(doc)}</p>
        <p className="mt-2 break-words font-mono text-sm text-ink">{doc.run.model}</p>
        <p className="mt-2 text-[11px] leading-5 text-muted">{modelNote(doc)}</p>
        <dl className="mt-6 divide-y divide-line/70">
          <MetaRow label="Mode" value={doc.run.model_mode} />
          <MetaRow label="Evidence tier" value={doc.run.evidence_tier} />
          <MetaRow label="Finished" value={utcTimestamp(doc.run.finished_at)} />
          {doc.run.model_recorded_at ? (
            <MetaRow label={doc.run.kind === "live" ? "Model output" : "Recorded"} value={utcTimestamp(doc.run.model_recorded_at)} />
          ) : null}
          <MetaRow label="Prompt tokens" value={doc.run.prompt_tokens.toLocaleString("en-US")} />
          <MetaRow label="Completion tokens" value={doc.run.completion_tokens.toLocaleString("en-US")} />
        </dl>
        <div className="mt-6 border-t border-line pt-5">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Model calls</p>
          <div className="mt-3 space-y-3">
            {doc.run.model_calls.map((call) => (
              <div key={`${call.purpose}-${call.latency_ms}`} className="grid grid-cols-[1fr_auto] gap-3">
                <div>
                  <p className="font-mono text-[11px] text-ink">{call.purpose}</p>
                  <p className="mt-0.5 font-mono text-[10px] text-faint">
                    {call.prompt_tokens ?? 0} prompt / {call.completion_tokens ?? 0} completion
                  </p>
                </div>
                <p className="font-mono text-[10px] text-muted">
                  {call.latency_ms ? `${(call.latency_ms / 1000).toFixed(1)}s` : "not recorded"}
                </p>
              </div>
            ))}
          </div>
        </div>
      </aside>
    </div>
  );
}

function PatchStage({ doc }: { doc: DemoExport }) {
  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_280px]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-line pb-4">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Candidate patch summary</p>
            <h3 className="mt-1 text-base font-medium tracking-tight text-ink">{doc.patch.summary}</h3>
          </div>
          <span className="font-mono text-[10px] text-faint">{doc.patch.changed_files.length} changed</span>
        </div>
        <p className="mt-3 text-[10px] leading-4 text-faint">
          Model-authored summary. The diff below is the reviewable source of truth.
        </p>
        <DiffViewer text={doc.patch.diff} />
      </div>
      <aside className="border-t border-line pt-5 lg:border-l lg:border-t-0 lg:pl-6 lg:pt-0">
        <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Changed files</p>
        <ul className="mt-2 space-y-2">
          {doc.patch.changed_files.map((file) => (
            <li key={file} className="break-all font-mono text-[11px] leading-5 text-muted">{file}</li>
          ))}
        </ul>
        <dl className="mt-6 divide-y divide-line/70 border-t border-line">
          <div className="py-3">
            <dt className="font-mono text-[10px] uppercase tracking-[0.1em] text-faint">Patch digest</dt>
            <dd className="mt-1 break-all font-mono text-[10px] leading-5 text-muted">
              {doc.patch.digest}
            </dd>
          </div>
          <MetaRow label="Candidate base" value={shortId(doc.run.base_revision, 10, 6)} />
          <MetaRow label="Model" value={doc.run.model} />
        </dl>
        <p className="mt-5 border-t border-line pt-4 text-[11px] leading-5 text-muted">
          Candidate output, shown exactly as generated. It was applied only to a disposable
          worktree and has not been merged.
        </p>
      </aside>
    </div>
  );
}

const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough",
  "for", "func", "go", "goto", "if", "interface", "map", "package", "range", "return",
  "select", "struct", "switch", "type", "var",
]);

function CodeText({ text }: { text: string }) {
  const tokens = text.split(
    /(`(?:\\.|[^`\\])*`|"(?:\\.|[^"\\])*"|\/\/[^\n]*|\b(?:break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|interface|map|package|range|return|select|struct|switch|type|var)\b)/g,
  );
  return (
    <>
      {tokens.map((token, index) => {
        if (!token) return null;
        if (token.startsWith("//")) {
          return <span key={`${index}-comment`} className="tok-comment">{token}</span>;
        }
        if (token.startsWith('"') || token.startsWith("`")) {
          return <span key={`${index}-string`} className="tok-string">{token}</span>;
        }
        if (GO_KEYWORDS.has(token)) {
          return <span key={`${index}-keyword`} className="tok-keyword">{token}</span>;
        }
        return <span key={`${index}-text`}>{token}</span>;
      })}
    </>
  );
}

function DiffViewer({ text }: { text: string }) {
  const lines = useMemo(() => parseDiff(text), [text]);
  return (
    <div className="mt-4 overflow-auto border border-line bg-surface" data-testid="demo-diff">
      <div className="min-w-[680px] font-mono text-[11px] leading-[1.8]">
        {lines.map((line, index) => (
          <div
            key={`${index}-${line.text}`}
            className={`grid grid-cols-[46px_46px_1fr] border-b border-line/50 last:border-0 ${
              line.kind === "add"
                ? "bg-[oklch(0.965_0.025_155)]"
                : line.kind === "del"
                  ? "bg-[oklch(0.965_0.025_28)]"
                  : line.kind === "hunk"
                    ? "bg-[oklch(0.965_0.012_80)]"
                    : ""
            }`}
          >
            <span className="select-none border-r border-line/50 px-2 py-0.5 text-right text-[10px] text-faint">
              {line.oldNo ?? ""}
            </span>
            <span className="select-none border-r border-line/50 px-2 py-0.5 text-right text-[10px] text-faint">
              {line.newNo ?? ""}
            </span>
            <span className={`whitespace-pre-wrap break-words px-3 py-0.5 ${
              line.kind === "add"
                ? "text-ok"
                : line.kind === "del"
                  ? "text-bad"
                  : line.kind === "hunk"
                    ? "text-accent"
                    : line.kind === "meta"
                      ? "text-faint"
                      : "text-muted"
            }`}>
              {line.kind === "meta" || line.kind === "hunk" ? (line.text || " ") : <CodeText text={line.text || " "} />}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function parseDiff(text: string): DiffLine[] {
  let oldNo = 0;
  let newNo = 0;
  return text.split("\n").map((line) => {
    if (line.startsWith("@@")) {
      const match = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (match) {
        oldNo = Number(match[1]);
        newNo = Number(match[2]);
      }
      return { kind: "hunk", text: line };
    }
    if (line.startsWith("---") || line.startsWith("+++") || line.startsWith("diff ") || line.startsWith("index ")) {
      return { kind: "meta", text: line };
    }
    if (line.startsWith("+")) {
      const entry = { kind: "add" as const, text: line, newNo };
      newNo += 1;
      return entry;
    }
    if (line.startsWith("-")) {
      const entry = { kind: "del" as const, text: line, oldNo };
      oldNo += 1;
      return entry;
    }
    const entry = { kind: "context" as const, text: line, oldNo, newNo };
    if (line.trim() !== "") {
      oldNo += 1;
      newNo += 1;
    }
    return entry;
  });
}

function ExecutionStage({ doc }: { doc: DemoExport }) {
  return (
    <div className="grid gap-10 lg:grid-cols-[1.05fr_.95fr]">
      <div>
        <div className="flex items-baseline justify-between gap-4 border-b border-line pb-4">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Verification ladder</p>
            <h3 className="mt-1 text-base font-medium tracking-tight">Candidate accepted for human review</h3>
          </div>
          <span className="font-mono text-[10px] text-ok">
            {doc.verification.ladder.accepted ? "accepted" : "not accepted"}
          </span>
        </div>
        <ol className="mt-2">
          {doc.verification.steps.map((step, index) => (
            <li key={step.name} className="grid grid-cols-[38px_1fr_auto] items-baseline gap-3 border-b border-line/80 py-3 last:border-0">
              <span className="font-mono text-[10px] text-faint">{String(index + 1).padStart(2, "0")}</span>
              <div>
                <p className="font-mono text-[12px] text-ink">{step.name}</p>
                <p className="mt-0.5 text-[10px] text-faint">
                  {step.passed ? "completed" : step.timed_out ? "timed out" : "failed"}
                  {step.exit_code !== 0 ? ` · exit ${step.exit_code}` : ""}
                </p>
              </div>
              <div className="text-right">
                <span className={step.passed ? "font-mono text-[10px] text-ok" : "font-mono text-[10px] text-bad"}>
                  {step.passed ? "PASS" : "FAIL"}
                </span>
                <p className="font-mono text-[10px] text-faint">{step.duration_ms.toLocaleString("en-US")} ms</p>
              </div>
            </li>
          ))}
        </ol>
        <div className="mt-7 border-t border-line pt-5">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Ladder state</p>
          <dl className="mt-2 divide-y divide-line/70">
            <MetaRow label="Diagnosis produced" value={<YesNo value={doc.verification.ladder.diagnosis_produced} />} />
            <MetaRow label="Patch generated" value={<YesNo value={doc.verification.ladder.patch_generated} />} />
            <MetaRow label="Patch applied" value={<YesNo value={doc.verification.ladder.patch_applied} />} />
            <MetaRow label="Build passed" value={<YesNo value={doc.verification.ladder.build_passed} />} />
            <MetaRow label="Trusted tests passed" value={<YesNo value={doc.verification.ladder.tests_passed} />} />
            <MetaRow label="Oracle verdict" value={<span className="font-mono text-ok">{doc.verification.ladder.oracle_verdict}</span>} />
          </dl>
        </div>
      </div>
      <aside className="border-t border-line pt-5 lg:border-l lg:border-t-0 lg:pl-7 lg:pt-0">
        <div className="flex items-center gap-2">
          <TerminalWindow size={16} weight="bold" className="text-accent" aria-hidden />
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Disposable sandbox</p>
        </div>
        <p className="mt-3 break-words font-mono text-[11px] leading-6 text-muted">{doc.verification.sandbox ?? "Sandbox details were not exported."}</p>
        <div className="mt-6 border-t border-line pt-5">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">What ran</p>
          <p className="mt-2 text-[11px] leading-5 text-muted">
            The candidate was built and tested inside the sandbox shown here. Command output
            remains in the local artifact directory and is not exposed in this public view.
          </p>
        </div>
        <div className="mt-6 border-t border-line pt-5">
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Candidate boundary</p>
          <p className="mt-2 text-[11px] leading-5 text-muted">
            The patch was applied only to a disposable worktree. The developer tree was not
            changed.
          </p>
        </div>
      </aside>
    </div>
  );
}

function OracleStage({
  doc,
  candidate,
  setCandidate,
}: {
  doc: DemoExport;
  candidate: CandidateMode;
  setCandidate: (mode: CandidateMode) => void;
}) {
  const active: DemoEval = candidate === "unsafe" ? doc.unsafe : doc.oracle;
  const unsafe = candidate === "unsafe";
  const failed = failedInvariants(active.invariants);
  const [filter, setFilter] = useState<"all" | "failures">(candidate === "unsafe" ? "failures" : "all");
  const capture = captureObservation(doc);
  const shown = filter === "failures" ? failed : active.invariants;
  const isVerified = active.verdict === "VERIFIED_FOR_TESTED_SCENARIOS";
  const passed = active.invariants.filter((item) => item.status === "PASS").length;
  const applicable = active.invariants.filter((item) => item.status !== "NOT_APPLICABLE").length;

  return (
    <div data-testid={unsafe ? "demo-unsafe" : "demo-oracle"}>
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-line pb-4">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Candidate</p>
          <div className="mt-2 inline-flex border border-line" role="group" aria-label="Candidate evidence">
            <button
              type="button"
              aria-pressed={!unsafe}
              onClick={() => setCandidate("verified")}
              className={`px-3 py-1.5 text-[11px] transition-colors ${
                !unsafe ? "bg-ink text-background" : "text-muted hover:text-ink"
              }`}
            >
              Repaired candidate
            </button>
            <button
              type="button"
              aria-pressed={unsafe}
              onClick={() => setCandidate("unsafe")}
              className={`border-l border-line px-3 py-1.5 text-[11px] transition-colors ${
                unsafe ? "bg-bad text-background" : "text-muted hover:text-ink"
              }`}
            >
              Unsafe seed
            </button>
          </div>
        </div>
        <p className="font-mono text-[10px] text-faint">
          {unsafe ? "seeded counterexample" : "accepted by the oracle"}
        </p>
      </div>

      <div className="grid gap-10 pt-7 lg:grid-cols-[1.05fr_.95fr]">
        <div>
          <p className={`text-[clamp(2.5rem,5vw,4.5rem)] font-semibold leading-none tracking-[-0.065em] ${isVerified ? "text-ok" : "text-bad"}`}>
            {isVerified ? "VERIFIED" : "UNSAFE"}
          </p>
          <p className="mt-3 font-mono text-[10px] uppercase tracking-[0.1em] text-faint">
            {active.verdict}
            {active.order_status ? ` · ${humanStatus(active.order_status)}` : ""}
          </p>
          <p className={`mt-2 text-[11px] ${isVerified ? "text-ok" : "text-bad"}`}>
            {unsafe
              ? `${failed.length} failed · ${passed} passed · ${active.invariants.length - applicable} N/A`
              : `${passed}/${applicable} applicable passed · ${active.invariants.length - applicable} N/A`}
          </p>
          <p className={`mt-2 border-l-2 pl-2 font-mono text-[10px] leading-4 ${isVerified ? "border-ok text-muted" : "border-bad text-bad"}`}>
            {unsafe
              ? `HISTORICAL ORACLE EVIDENCE · ${doc.unsafe.fresh_trusted_gate.executed ? "fresh trusted gate below" : "fresh trusted gate not run"}`
              : `TESTED ${doc.run.evidence_tier === "R" ? "PROVIDER" : "FIXTURE"} SCENARIO ONLY · TIER ${doc.run.evidence_tier}`}
          </p>
          <p className="mt-6 max-w-[62ch] text-[13px] leading-6 text-muted">{active.reason}</p>
          {unsafe ? (
            <div className="mt-6 border-t border-line pt-4">
              <p className="text-[11px] leading-5 text-muted">
                Historical independent-oracle evidence. {doc.unsafe.provenance}
              </p>
              <p className="mt-1 font-mono text-[10px] text-faint">source: {doc.unsafe.source}</p>
              <div className="mt-3 border-l-2 border-bad pl-3">
                {doc.unsafe.fresh_trusted_gate.executed ? (
                  <>
                    <p className="text-[11px] font-medium text-ink">
                      Fresh trusted gate: {humanStatus(doc.unsafe.fresh_trusted_gate.result)}
                    </p>
                    <p className="mt-1 text-[11px] leading-5 text-muted">{doc.unsafe.fresh_trusted_gate.detail}</p>
                  </>
                ) : (
                  <p className="text-[11px] leading-5 text-muted">
                    Historical independent-oracle evidence. The fresh trusted gate was not run
                    for this recorded artifact.
                  </p>
                )}
              </div>
            </div>
          ) : null}
          <div className="mt-7 border-t border-line pt-4">
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">What the oracle established</p>
            <p className="mt-2 max-w-[62ch] text-[11px] leading-5 text-muted">
              The compiled candidate fixture was replayed, the durable ledger was re-read, and
              the listed financial invariants were evaluated for this scenario. This is tier
              {doc.run.evidence_tier} evidence, not a claim about live PayPal behavior or untested
              scenarios.
            </p>
          </div>
          {!unsafe && capture ? (
            <div className="mt-6 flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-line pt-4 font-mono text-[10px] text-muted">
              <span>capture {capture.capture_id}</span>
              <span>{capture.capture_status}</span>
              <span>{capture.capture_amount} {capture.capture_currency}</span>
            </div>
          ) : null}
        </div>

        <div>
          <div className="flex items-center justify-between gap-3 border-b border-line pb-3">
            <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Invariants</p>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setFilter("all")}
                className={`text-[10px] ${filter === "all" ? "text-ink" : "text-faint hover:text-muted"}`}
              >
                All {active.invariants.length}
              </button>
              <span className="text-line-strong">/</span>
              <button
                type="button"
                onClick={() => setFilter("failures")}
                className={`text-[10px] ${filter === "failures" ? "text-bad" : "text-faint hover:text-muted"}`}
              >
                Failures {failed.length}
              </button>
            </div>
          </div>
          <p className="border-b border-line/70 py-2 font-mono text-[9px] text-faint">
            PASS = invariant holds · N/A = not applicable to this fixture
          </p>
          <InvariantList items={shown} />
        </div>
      </div>
    </div>
  );
}

function InvariantList({ items }: { items: DemoInvariant[] }) {
  if (!items.length) {
    return <p className="py-5 text-[11px] leading-5 text-faint">No invariant records in this view.</p>;
  }
  return (
    <ul>
      {items.map((item) => {
        const failed = item.status === "FAIL";
        return (
          <li key={item.id} className="grid grid-cols-[36px_76px_1fr] gap-3 border-b border-line/80 py-3 last:border-0">
            <span className={`font-mono text-[10px] ${failed ? "text-bad" : item.status === "PASS" ? "text-ok" : "text-faint"}`}>
              {item.id}
            </span>
            <span className={`font-mono text-[10px] ${failed ? "text-bad" : item.status === "PASS" ? "text-ok" : "text-faint"}`}>
              {item.status}
            </span>
            <span className={`text-[11px] leading-5 ${failed ? "text-bad" : "text-muted"}`}>{item.summary}</span>
          </li>
        );
      })}
    </ul>
  );
}

function ReviewStage({ doc, onCompare }: { doc: DemoExport; onCompare: () => void }) {
  const human = doc.human_review;
  return (
    <div className="grid gap-10 lg:grid-cols-[1.1fr_.9fr]" data-testid="demo-human">
      <div>
        <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Human boundary</p>
        <h3 className="mt-3 max-w-[760px] text-balance text-3xl font-semibold leading-[1.05] tracking-[-0.045em] sm:text-4xl">
          The candidate is ready for review.
          <br />
          <span className="text-accent">No approval has been recorded.</span>
        </h3>
        <p className="mt-5 max-w-[64ch] text-[13px] leading-6 text-muted">
          Waypoint stops at a reviewable candidate. A human decides whether the change is
          integrated; this page cannot merge, deploy, capture, refund, or retry a payment.
        </p>
        <div className="mt-8 grid gap-px border-y border-line bg-line sm:grid-cols-2">
          <BoundaryFact label="Candidate patch" value="Disposable worktree only" />
          <BoundaryFact label="Developer tree" value="Unchanged" />
          <BoundaryFact label="Deployment" value="Not performed" />
          <BoundaryFact label="Human approval" value="Still required" />
        </div>
        <p className="mt-4 text-[11px] leading-5 text-faint">
          No merge, push, deploy, capture, refund, or payment retry exists in this run.
          The review console is read-only for payment state.
        </p>
      </div>
      <aside className="border-t border-line pt-5 lg:border-l lg:border-t-0 lg:pl-7 lg:pt-0">
        <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Review status</p>
        <p className="mt-2 break-words font-mono text-sm text-ink">{humanStatus(human.status)}</p>
        <dl className="mt-6 divide-y divide-line/70">
          <MetaRow label="Candidate applied in worktree" value={<YesNo value={human.candidate_patch_applied_in_disposable_worktree} />} />
          <MetaRow label="Developer tree applied" value={<YesNo value={human.developer_tree_applied} />} />
          <MetaRow label="Deployed" value={<YesNo value={human.deployed} />} />
          <MetaRow label="Human approval required" value={<YesNo value={human.human_approval_required} />} />
        </dl>
        <p className="mt-6 border-t border-line pt-4 text-[11px] leading-5 text-muted">
          This view is read-only. Review recording is disabled here, so no visitor can record an
          approval from this page.
        </p>
        <button
          type="button"
          onClick={onCompare}
          className="mt-5 inline-flex items-center gap-2 border border-line px-3 py-2 text-[11px] font-medium text-muted transition-colors hover:border-line-strong hover:text-ink"
        >
          Inspect the unsafe candidate
          <ArrowRight size={13} weight="bold" />
        </button>
      </aside>
    </div>
  );
}

function BoundaryFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-background px-4 py-4">
      <p className="font-mono text-[10px] uppercase tracking-[0.1em] text-faint">{label}</p>
      <p className="mt-1.5 text-[12px] text-ink">{value}</p>
    </div>
  );
}

function MetaRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-2.5">
      <dt className="text-[10px] leading-4 text-faint">{label}</dt>
      <dd className="min-w-0 break-words text-right font-mono text-[10px] leading-4 text-muted">{value}</dd>
    </div>
  );
}

function YesNo({ value }: { value?: boolean }) {
  if (value === undefined) return <span className="text-faint">not recorded</span>;
  return <span className={value ? "text-ok" : "text-muted"}>{value ? "yes" : "no"}</span>;
}

function CopyValue({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    void navigator.clipboard?.writeText(value).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    }).catch(() => {
      setCopied(false);
    });
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="break-all font-mono text-[10px]">{value}</span>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? "Copied" : "Copy value"}
        className="shrink-0 text-faint transition-colors hover:text-ink"
      >
        {copied ? <Check size={12} weight="bold" /> : <Copy size={12} weight="bold" />}
      </button>
    </span>
  );
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="border-y border-line py-10">
      <p className="text-sm font-medium text-ink">{title}</p>
      <p className="mt-2 max-w-[55ch] text-[12px] leading-6 text-muted">{body}</p>
    </div>
  );
}

function EvidenceSkeleton() {
  return (
    <div className="py-10" aria-busy="true" aria-live="polite">
      <div className="h-2.5 w-40 animate-pulse bg-line motion-reduce:animate-none" />
      <div className="mt-4 h-12 w-3/5 animate-pulse bg-line/70 motion-reduce:animate-none" />
      <div className="mt-3 h-3 w-2/5 animate-pulse bg-line/50 motion-reduce:animate-none" />
      <div className="mt-8 grid grid-cols-3 border-y border-line sm:grid-cols-6">
        {[0, 1, 2, 3, 4, 5].map((item) => (
          <div key={item} className="border-r border-line px-3 py-4 last:border-r-0 sm:px-4">
            <div className="h-2 w-5 animate-pulse bg-line motion-reduce:animate-none" />
            <div className="mt-2 h-3 w-16 animate-pulse bg-line/70 motion-reduce:animate-none" />
            <div className="mt-1.5 h-2 w-12 animate-pulse bg-line/50 motion-reduce:animate-none" />
          </div>
        ))}
      </div>
      <div className="mt-8 grid gap-8 lg:grid-cols-[1.1fr_.9fr]">
        <div className="border border-line bg-surface p-5">
          <div className="h-2.5 w-28 animate-pulse bg-line/70 motion-reduce:animate-none" />
          <div className="mt-4 h-6 w-4/5 animate-pulse bg-line/60 motion-reduce:animate-none" />
          <div className="mt-3 space-y-2">
            <div className="h-2.5 w-full animate-pulse bg-line/50 motion-reduce:animate-none" />
            <div className="h-2.5 w-11/12 animate-pulse bg-line/50 motion-reduce:animate-none" />
            <div className="h-2.5 w-3/4 animate-pulse bg-line/50 motion-reduce:animate-none" />
          </div>
          <div className="mt-6 border-t border-line pt-4">
            <div className="h-2.5 w-24 animate-pulse bg-line/60 motion-reduce:animate-none" />
            <div className="mt-3 space-y-2">
              <div className="h-2.5 w-full animate-pulse bg-line/40 motion-reduce:animate-none" />
              <div className="h-2.5 w-10/12 animate-pulse bg-line/40 motion-reduce:animate-none" />
            </div>
          </div>
        </div>
        <div className="border border-line bg-surface p-5">
          <div className="h-2.5 w-24 animate-pulse bg-line/70 motion-reduce:animate-none" />
          <div className="mt-4 divide-y divide-line/70">
            {[0, 1, 2, 3, 4].map((item) => (
              <div key={item} className="flex justify-between gap-4 py-3">
                <div className="h-2.5 w-24 animate-pulse bg-line/50 motion-reduce:animate-none" />
                <div className="h-2.5 w-20 animate-pulse bg-line/40 motion-reduce:animate-none" />
              </div>
            ))}
          </div>
        </div>
      </div>
      <p className="mt-4 font-mono text-[10px] text-faint">Loading evidence file.</p>
    </div>
  );
}

function EvidenceError({ message }: { message: string }) {
  return (
    <div className="mx-auto max-w-[640px] py-24">
      <WarningCircle size={24} weight="bold" className="text-bad" aria-hidden />
      <h1 className="mt-4 text-2xl font-semibold tracking-tight">Evidence unavailable</h1>
      <p className="mt-3 text-sm leading-6 text-muted">
        The public case file expects the committed sanitized evidence at{" "}
        <span className="font-mono text-[12px] text-ink">public/demo/recorded.json</span>. Run <span className="font-mono text-[12px] text-ink">make demo</span> for a fresh local
        replay.
      </p>
      {message ? <p className="mt-3 font-mono text-[10px] leading-5 text-faint">{message}</p> : null}
    </div>
  );
}

export { Showcase };
