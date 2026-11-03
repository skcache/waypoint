"use client";

import { useEffect, useState } from "react";
import type { DemoExport, DemoInvariant, DemoObservation } from "@/lib/demo";

function Panel({
  title,
  meta,
  children,
  testId,
}: {
  title: string;
  meta?: React.ReactNode;
  children: React.ReactNode;
  testId?: string;
}) {
  return (
    <section
      data-testid={testId}
      className="overflow-hidden rounded-panel border border-line bg-surface"
    >
      <header className="flex h-9 items-center justify-between gap-3 border-b border-line px-3.5">
        <h2 className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted">{title}</h2>
        {meta}
      </header>
      <div className="p-3.5">{children}</div>
    </section>
  );
}

function Badge({ children, tone = "border-line text-muted" }: { children: React.ReactNode; tone?: string }) {
  return (
    <span className={`rounded border px-1.5 py-0.5 text-[10px] font-medium ${tone}`}>{children}</span>
  );
}

function InvariantList({ items }: { items?: DemoInvariant[] }) {
  if (!items?.length) return <p className="text-[11px] text-faint">No invariant records.</p>;
  return (
    <ul className="space-y-1.5">
      {items.map((inv) => (
        <li key={inv.id} className="flex gap-2 text-[11px] leading-5">
          <span
            className={`mono w-8 shrink-0 ${
              inv.status === "FAIL" ? "text-bad" : inv.status === "PASS" ? "text-ok" : "text-faint"
            }`}
          >
            {inv.id}
          </span>
          <span className="w-24 shrink-0 text-muted">{inv.status}</span>
          <span className="text-muted">{inv.summary}</span>
        </li>
      ))}
    </ul>
  );
}

function Diff({ text }: { text: string }) {
  return (
    <pre
      data-testid="demo-diff"
      className="mono max-h-[460px] overflow-auto rounded-md border border-line bg-background p-3 text-[11px] leading-[1.65]"
    >
      {text.split("\n").map((line, i) => {
        const tone = line.startsWith("+")
          ? "text-ok"
          : line.startsWith("-")
            ? "text-bad"
            : line.startsWith("@@")
              ? "text-accent"
              : "text-muted";
        return (
          <span key={i} className={`block whitespace-pre-wrap break-words ${tone}`}>
            {line || " "}
          </span>
        );
      })}
    </pre>
  );
}

function Observations({ items }: { items: DemoObservation[] }) {
  if (!items.length) return <p className="text-[11px] text-faint">No provider observations exported.</p>;
  return (
    <ol className="divide-y divide-line/70">
      {items.map((o) => (
        <li key={`${o.seq}-${o.method}`} className="py-2 first:pt-0 last:pb-0">
          <div className="flex items-baseline justify-between gap-2">
            <span className="mono text-[11px] text-ink">
              {String(o.seq).padStart(2, "0")} · {o.method}
            </span>
            <span className="mono text-[10px] text-faint">
              {o.tier ?? "F"} · {o.order_status ?? "—"}
            </span>
          </div>
          <div className="mt-1 flex flex-wrap gap-x-2 gap-y-0.5 text-[10px] text-faint">
            {o.request_id ? <span className="mono">req {o.request_id.slice(0, 12)}</span> : null}
            {o.mutation_applied ? <span className="text-ok">applied</span> : null}
            {o.idempotent_replay ? <span className="text-accent">replayed</span> : null}
            {o.response_dropped ? <span className="text-warn">response lost</span> : null}
            {o.definite_refusal ? <span className="text-bad">refused</span> : null}
            {o.capture_id ? <span className="mono">cap {o.capture_id.slice(0, 12)}</span> : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

function verdictTone(v: string) {
  return v === "VERIFIED_FOR_TESTED_SCENARIOS"
    ? "text-ok"
    : v === "UNSAFE"
      ? "text-bad"
      : "text-warn";
}

export default function DemoPage() {
  const [doc, setDoc] = useState<DemoExport | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    let live = true;
    async function load() {
      for (const path of ["/demo/local.json", "/demo/recorded.json"]) {
        const res = await fetch(path, { cache: "no-store" });
        if (!res.ok) continue;
        const data = (await res.json()) as DemoExport;
        if (!live) return;
        setDoc(data);
        return;
      }
      if (live) setErr("No demo export found. Run `make demo` or open the recorded export.");
    }
    load().catch((e) => {
      if (live) setErr(e instanceof Error ? e.message : "Could not load the demo export.");
    });
    return () => {
      live = false;
    };
  }, []);

  if (err) {
    return (
      <div className="rounded-panel border border-line bg-surface p-6">
        <p className="text-sm font-medium">Repair run unavailable</p>
        <p className="mt-2 text-xs leading-5 text-muted">{err}</p>
      </div>
    );
  }
  if (!doc) {
    return <p className="text-xs text-faint">Loading repair evidence…</p>;
  }

  const { run, incident, pre_repair: pre, diagnosis, patch, verification, oracle, unsafe, human_review: human } = doc;
  const observations = incident.observations ?? [];
  const isLocal = run.kind === "replay" || run.kind === "live";

  return (
    <div className="space-y-6">
      <header className="border-b border-line pb-5">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-faint">Waypoint / repair run</p>
          <Badge tone={isLocal ? "border-accent/40 text-accent" : "border-line text-muted"}>
            {run.kind === "replay" ? "LOCAL REPLAY" : run.kind === "live" ? "LIVE RUN" : "RECORDED RUN"}
          </Badge>
          <Badge>tier F</Badge>
        </div>
        <h1 className="mt-2 text-xl font-semibold tracking-tight">{patch.summary || "Payment integration repair"}</h1>
        <p className="mt-1.5 text-xs text-muted">
          {run.source_label || run.source} · {run.model_mode} · {run.model}
        </p>
        <dl className="mt-4 grid gap-x-6 gap-y-1 text-[11px] sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Run</dt>
            <dd className="mono mt-0.5 text-ink" data-testid="demo-run-id">{run.run_id}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Base</dt>
            <dd className="mono mt-0.5 text-ink">{run.base_revision.slice(0, 16)}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Run finished</dt>
            <dd className="mono mt-0.5 text-ink">{run.finished_at}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Model recorded</dt>
            <dd className="mono mt-0.5 text-ink">{run.model_recorded_at ?? "not recorded for this run"}</dd>
          </div>
        </dl>
        <p className="mt-3 text-[11px] leading-5 text-faint">
          Tokens: {run.prompt_tokens.toLocaleString()} prompt / {run.completion_tokens.toLocaleString()} completion ·{" "}
          {run.usage_source}. Every observation is a deterministic scripted fixture; no PayPal transaction was made.
        </p>
      </header>

      <div className="grid gap-4 lg:grid-cols-12">
        <div className="space-y-4 lg:col-span-4">
          <Panel title="Incident" testId="demo-incident" meta={<Badge>{incident.fault}</Badge>}>
            <dl className="space-y-1.5 text-[11px]">
              <div className="flex justify-between gap-3"><dt className="text-muted">Order</dt><dd className="mono">{incident.order_id}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Status</dt><dd className="mono">{incident.order_status}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Evidence</dt><dd className="mono">{incident.evidence_tier}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Amount</dt><dd className="mono">{incident.total_minor} {incident.currency}</dd></div>
            </dl>
            <div className="mt-3 border-t border-line pt-3">
              <p className="text-[10px] uppercase tracking-wider text-faint">Pre-repair verdict</p>
              <p className={`mono mt-1 text-[11px] ${verdictTone(pre.verdict)}`}>{pre.verdict} · {pre.order_status}</p>
              <p className="mt-1 text-[11px] leading-5 text-muted">{pre.reason}</p>
              <div className="mt-2"><InvariantList items={(pre.invariants ?? []).filter((i) => i.status !== "PASS" && i.status !== "NOT_APPLICABLE")} /></div>
            </div>
          </Panel>

          <Panel title="Provider observations" meta={<span className="mono text-[10px] text-faint">{observations.length} events</span>}>
            <Observations items={observations} />
          </Panel>
        </div>

        <div className="space-y-4 lg:col-span-8">
          <div className="grid gap-4 md:grid-cols-2">
            <Panel title="Diagnosis">
              {diagnosis ? (
                <div className="space-y-3">
                  <div>
                    <p className="text-[10px] uppercase tracking-wider text-faint">Root cause</p>
                    <p className="mt-1 text-xs leading-5 text-ink">{diagnosis.root_cause}</p>
                  </div>
                  <div>
                    <p className="text-[10px] uppercase tracking-wider text-faint">Mechanism</p>
                    <p className="mt-1 text-[11px] leading-5 text-muted">{diagnosis.mechanism}</p>
                  </div>
                  <div>
                    <p className="text-[10px] uppercase tracking-wider text-faint">Proposed fix</p>
                    <p className="mt-1 text-[11px] leading-5 text-muted">{diagnosis.proposed_fix}</p>
                  </div>
                  <p className="mono text-[10px] text-faint">{diagnosis.suspected_files.join(" · ")}</p>
                </div>
              ) : (
                <p className="text-[11px] text-faint">No diagnosis exported.</p>
              )}
            </Panel>

            <Panel title="Verification" meta={<span className={`text-[10px] ${verdictTone(verification.ladder.oracle_verdict)}`}>{verification.ladder.accepted ? "accepted" : "not accepted"}</span>}>
              <ul className="divide-y divide-line/70">
                {[
                  ["Diagnosis", verification.ladder.diagnosis_produced],
                  ["Patch generated", verification.ladder.patch_generated],
                  ["Patch applied (worktree)", verification.ladder.patch_applied],
                  ["Build passed", verification.ladder.build_passed],
                  ["Trusted tests passed", verification.ladder.tests_passed],
                ].map(([label, value]) => (
                  <li key={String(label)} className="flex justify-between gap-3 py-1.5 text-[11px]">
                    <span className="text-muted">{label}</span>
                    <span className={value ? "text-ok" : "text-bad"}>{value ? "yes" : "no"}</span>
                  </li>
                ))}
              </ul>
              <ol className="mt-3 space-y-1 border-t border-line pt-2">
                {verification.steps.map((s) => (
                  <li key={s.name} className="flex justify-between gap-3 text-[10px]">
                    <span className="mono text-muted">{s.name}</span>
                    <span className={s.passed ? "text-ok" : "text-bad"}>
                      exit {s.exit_code} · {s.duration_ms} ms
                    </span>
                  </li>
                ))}
              </ol>
            </Panel>
          </div>

          <Panel title="Generated diff" meta={<span className="mono text-[10px] text-faint">{patch.digest.slice(0, 23)}…</span>}>
            <p className="mb-2 text-[11px] text-muted">{patch.changed_files.join(" · ")}</p>
            <Diff text={patch.diff} />
          </Panel>

          <div className="grid gap-4 md:grid-cols-2">
            <Panel title="Independent oracle" meta={<Badge tone={verdictTone(oracle.verdict)}>{oracle.verdict}</Badge>} testId="demo-oracle">
              <p className="text-[11px] leading-5 text-muted">{oracle.reason}</p>
              <div className="mt-2"><InvariantList items={oracle.invariants ?? []} /></div>
            </Panel>

            <Panel title="Unsafe seed" meta={<Badge tone="border-bad/40 text-bad">SEEDED</Badge>} testId="demo-unsafe">
              <p className="text-[11px] leading-5 text-muted">{unsafe.provenance}</p>
              <p className="mono mt-1 text-[10px] text-faint">source: {unsafe.source}</p>
              <p className="mt-2 text-[11px] text-bad">{unsafe.reason}</p>
              <div className="mt-2"><InvariantList items={(unsafe.invariants ?? []).filter((i) => i.status === "FAIL")} /></div>
              <div className="mt-3 border-t border-line pt-2 text-[11px] leading-5">
                {unsafe.fresh_trusted_gate.executed ? (
                  <>
                    <p className="text-ink">Fresh trusted gate: {unsafe.fresh_trusted_gate.result}</p>
                    <p className="text-faint">{unsafe.fresh_trusted_gate.detail}</p>
                  </>
                ) : (
                  <p className="text-faint">Fresh execution: not run for this recorded artifact.</p>
                )}
              </div>
            </Panel>
          </div>

          <Panel title="Human review" meta={<Badge tone="border-line text-muted">{human.status}</Badge>} testId="demo-human">
            <dl className="grid gap-x-6 gap-y-1 text-[11px] sm:grid-cols-2">
              <div className="flex justify-between gap-3"><dt className="text-muted">Candidate patch in disposable worktree</dt><dd className={human.candidate_patch_applied_in_disposable_worktree ? "text-ok" : "text-faint"}>{human.candidate_patch_applied_in_disposable_worktree ? "yes" : "no"}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Developer tree applied</dt><dd className={human.developer_tree_applied ? "text-bad" : "text-ok"}>{human.developer_tree_applied ? "yes" : "no"}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Deployed</dt><dd className={human.deployed ? "text-bad" : "text-ok"}>{human.deployed ? "yes" : "no"}</dd></div>
              <div className="flex justify-between gap-3"><dt className="text-muted">Human approval required</dt><dd className={human.human_approval_required ? "text-ink" : "text-faint"}>{human.human_approval_required ? "yes" : "no"}</dd></div>
            </dl>
            <p className="mt-3 border-t border-line pt-2 text-[11px] leading-5 text-faint">
              No merge, push, deploy, capture, refund, or payment retry exists in this run. The review console is
              read-only for payment state.
            </p>
          </Panel>
        </div>
      </div>
    </div>
  );
}
