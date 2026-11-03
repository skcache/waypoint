import Link from "next/link";
import { notFound } from "next/navigation";
import { DecisionForm } from "@/components/decision-form";
import {
  caseById,
  data,
  fmtMs,
  fmtTokens,
  observations,
  runFor,
  shortHash,
  statusLabel,
  statusTone,
  usd,
} from "@/lib/m5";

// The condition switcher reads a search param; this is a review console, not a
// cacheable marketing page, so allow the blocking render.
export const instant = false;

export function generateStaticParams() {
  return data.cases.map((row) => ({ id: row.case.id }));
}

function Mono({ children }: { children: React.ReactNode }) {
  return <span className="mono text-[13px] text-muted">{children}</span>;
}

function Panel({
  title,
  meta,
  children,
}: {
  title: string;
  meta?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="overflow-hidden rounded-panel border border-line bg-surface">
      <header className="flex h-10 items-center justify-between gap-3 border-b border-line px-4">
        <h2 className="text-[14px] font-medium text-muted">{title}</h2>
        {meta}
      </header>
      <div className="p-4">{children}</div>
    </section>
  );
}

function Diff({ text }: { text: string }) {
  if (!text) return <p className="text-[13px] text-faint">No patch was produced.</p>;
  return (
    <pre className="mono max-h-[520px] overflow-auto rounded-[8px] border border-line bg-background p-3 text-[13px] leading-[1.65]">
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

function Ladder({ run }: { run: ReturnType<typeof runFor> }) {
  if (!run) return null;
  const report = run.report;
  const evalDoc = run.eval;
  const rows: [string, boolean | string | undefined][] = [
    ["Diagnosis produced", report?.final_ladder?.diagnosis_produced as boolean | undefined],
    ["Patch generated", report?.final_ladder?.patch_generated as boolean | undefined],
    ["Patch applied", run.patch_applied],
    ["Build passed", run.build_passed],
    ["Tests passed", run.tests_passed],
    ["Checker verdict", run.oracle_verdict || "—"],
    ["Accepted for review", run.accepted],
  ];
  return (
    <div className="space-y-3">
      <dl className="divide-y divide-line">
        {rows.map(([label, value]) => (
          <div key={label} className="flex items-baseline justify-between gap-3 py-2">
            <dt className="text-[14px] text-muted">{label}</dt>
            <dd
              className={`min-w-0 break-all text-right text-[14px] ${
                value === true
                  ? "text-ok"
                  : value === false
                    ? "text-faint"
                    : String(value).includes("UNSAFE")
                      ? "text-bad"
                      : String(value).includes("VERIFIED")
                        ? "text-ok"
                        : "text-ink"
              }`}
            >
              {value === true ? "Yes" : value === false ? "No" : String(value ?? "—")}
            </dd>
          </div>
        ))}
      </dl>
      {evalDoc?.accepted === false && evalDoc.reason ? (
        <p className="rounded-[8px] border border-line bg-background px-3 py-2 text-[13px] leading-5 text-muted">
          {evalDoc.reason}
        </p>
      ) : null}
      <p className="text-[13px] leading-5 text-faint">
        {fmtMs(run.latency_ms)} · {fmtTokens(run.prompt_tokens)} tokens in,{" "}
        {fmtTokens(run.completion_tokens)} out · {usd(run.estimated_usd)}
      </p>
    </div>
  );
}

export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ cond?: string }>;
}) {
  const { id } = await params;
  const { cond } = await searchParams;
  const row = caseById(id);
  if (!row) notFound();
  const conditions = ["B1", "B2", "B0"].filter((c) => row.runs[c] || data.views[id]?.[c]);
  const active = cond && conditions.includes(cond) ? cond : conditions.includes("B2") ? "B2" : conditions[0];
  const run = runFor(id, active);
  const obs = observations(id, active);
  const pre = row.pre_repair;
  const diag = run?.report?.diagnosis;
  const evalInvariants = (run?.eval?.oracle?.invariants ?? []).filter(
    (i) => i.status === "FAIL" || i.status === "INCONCLUSIVE",
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4 border-b border-line pb-4">
        <div>
          <Link href="/" className="text-[13px] text-faint hover:text-ink">
            ← Incidents
          </Link>
          <h1 className="mt-2 text-[24px] font-medium leading-[1.2]">{row.case.title}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] text-faint">
            <Mono>{row.case.id}</Mono>
            <span className="text-line-strong">/</span>
            <span>{row.case.family}</span>
            <span className="text-line-strong">/</span>
            <span>{row.case.exposed ? "exposed reference" : "held-out"}</span>
            <span className="text-line-strong">/</span>
            <span>tier F</span>
          </div>
        </div>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-right">
          <div>
            <dt className="text-[13px] text-faint">Base</dt>
            <dd className="mono text-[13px]">{shortHash(row.mutation_sha || data.base_sha)}</dd>
          </div>
          <div>
            <dt className="text-[13px] text-faint">Model</dt>
            <dd className="mono text-[13px]">{data.model.id ?? "not configured"}</dd>
          </div>
        </dl>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {conditions.map((c) => (
          <Link
            key={c}
            href={`/incidents/${encodeURIComponent(id)}?cond=${c}`}
            className={`inline-flex min-h-11 items-center rounded-[8px] border px-3 text-[14px] transition-colors ${
              active === c
                ? "border-line-strong bg-surface text-ink"
                : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {c === "B0" ? "B0 · Rules only" : c === "B1" ? "B1 · One attempt" : "B2 · With feedback"}
          </Link>
        ))}
        <span className="ml-auto text-[13px] text-faint">
          Before repair: <span className="mono">{pre.verdict}</span> · {pre.order_status}
        </span>
      </div>

      <div className="grid gap-4 lg:grid-cols-12">
        <div className="space-y-4 lg:col-span-3">
          <Panel
            title="Provider observations"
            meta={<Mono>{obs.length} events</Mono>}
          >
            {obs.length === 0 ? (
              <p className="text-[13px] leading-5 text-faint">
                The run files hold the provider events. No event file is attached to this condition.
              </p>
            ) : (
              <ol className="space-y-0">
                {obs.map((o, i) => (
                  <li key={i} className="border-b border-line/70 py-2 first:pt-0 last:border-0">
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="mono text-[13px] text-ink">
                        {String(o.Seq ?? i + 1).padStart(2, "0")} · {o.Method}
                      </span>
                      <span className="mono text-[13px] text-faint">
                        {o.Tier ?? "F"} · {o.OrderStatus ?? "—"}
                      </span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-2 gap-y-0.5 text-[13px] text-faint">
                      {o.RequestID ? <span className="mono">req {shortHash(o.RequestID)}</span> : null}
                      {o.Applied ? <span className="text-ok">applied</span> : null}
                      {o.Replayed ? <span className="text-accent">replayed</span> : null}
                      {o.ResponseDropped ? <span className="text-warn">response lost</span> : null}
                      {o.DefiniteRefusal ? <span className="text-bad">refused</span> : null}
                      {o.CaptureID ? <span className="mono">cap {shortHash(o.CaptureID)}</span> : null}
                      {o.CaptureStatus ? <span>{o.CaptureStatus}</span> : null}
                    </div>
                  </li>
                ))}
              </ol>
            )}
          </Panel>

          <Panel title="Result before repair">
            <p className="text-[13px] leading-5 text-muted">
              <span className="mono text-ink">{pre.verdict}</span> · {pre.order_status}
            </p>
            {pre.failing?.length ? (
              <ul className="mt-2 space-y-2">
                {pre.failing.map((f) => (
                  <li key={f} className="text-[13px] leading-5 text-bad">
                    {f}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-[13px] text-faint">No counterexample before repair.</p>
            )}
          </Panel>
        </div>

        <div className="space-y-4 lg:col-span-6">
          <Panel
            title="AI diagnosis"
            meta={<Mono>{run?.diagnosis_category ?? "not run"}</Mono>}
          >
            {diag ? (
              <div className="space-y-3">
                <div>
                  <p className="text-[13px] font-medium text-ink/70">Root cause</p>
                  <p className="mt-1 text-[14px] leading-6 text-ink">{diag.root_cause}</p>
                </div>
                <div>
                  <p className="text-[13px] font-medium text-ink/70">Mechanism</p>
                  <p className="mt-1 text-[13px] leading-5 text-muted">{diag.mechanism}</p>
                </div>
                <div>
                  <p className="text-[13px] font-medium text-ink/70">Proposed fix</p>
                  <p className="mt-1 text-[13px] leading-5 text-muted">{diag.proposed_fix}</p>
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px] text-faint">
                  {diag.suspected_files?.length ? (
                    <span className="mono">{diag.suspected_files.join(" · ")}</span>
                  ) : null}
                  {diag.supporting_evidence?.length ? (
                    <span>evidence {diag.supporting_evidence.join(", ")}</span>
                  ) : null}
                  {diag.invariants_at_risk?.length ? (
                    <span>rules {diag.invariants_at_risk.join(", ")}</span>
                  ) : null}
                </div>
                {diag.uncertainties?.length ? (
                  <div className="border-t border-line pt-2">
                    <p className="text-[13px] font-medium text-ink/70">What the AI cannot know</p>
                    <ul className="mt-1 list-disc space-y-1 pl-4 text-[13px] leading-5 text-faint">
                      {diag.uncertainties.map((u) => (
                        <li key={u}>{u}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </div>
            ) : (
              <p className="text-[13px] text-faint">
                {run?.status === "DIAGNOSIS_ONLY"
                  ? "B0 gives a cause category only. It cannot write a patch."
                  : "No diagnosis for this condition."}
              </p>
            )}
          </Panel>

          <Panel title="Patch" meta={<Mono>{run?.diff ? `${run.diff.split("\n").length} lines` : "none"}</Mono>}>
            <Diff text={run?.diff ?? ""} />
          </Panel>
        </div>

        <div className="space-y-4 lg:col-span-3">
          <Panel
            title="Checker result"
            meta={
              <span className={`text-[13px] font-medium ${statusTone(run?.status ?? "")}`}>
                {run ? statusLabel(run.status) : "not run"}
              </span>
            }
          >
            <Ladder run={run} />
          </Panel>

          <Panel title="Failed rules">
            {evalInvariants.length === 0 ? (
              <p className="text-[13px] leading-5 text-faint">
                No rule failed in this condition.
              </p>
            ) : (
              <ul className="space-y-2">
                {evalInvariants.map((inv) => (
                  <li key={inv.id}>
                    <p className="mono text-[13px] text-bad">
                      {inv.id} · {inv.status}
                    </p>
                    <p className="mt-0.5 text-[13px] leading-5 text-muted">{inv.summary}</p>
                  </li>
                ))}
              </ul>
            )}
          </Panel>

          <Panel title="Human decision">
            <DecisionForm caseId={row.case.id} condition={active} />
            <p className="mt-3 border-t border-line pt-2 text-[13px] leading-5 text-faint">
              The console is read-only for payment state. Recording is off unless an operator sets a reviewer token. A recorded decision is a local note. It does not merge, deploy, capture, refund, or retry a payment.
            </p>
          </Panel>
        </div>
      </div>
    </div>
  );
}
