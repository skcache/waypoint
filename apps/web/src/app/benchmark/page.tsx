import Link from "next/link";
import {
  data,
  fmtTokens,
  statusLabel,
  statusTone,
  usd,
} from "@/lib/m5";

export const metadata = { title: "Benchmark" };

const conditionLabel: Record<string, string> = {
  B0: "B0 · deterministic rules",
  B1: "B1 · single-shot AI",
  B2: "B2 · oracle-guided AI",
};

export default function Page() {
  const heldOut = data.cases.filter((c) => !c.case.exposed);
  const families = Array.from(new Set(heldOut.map((c) => c.case.family)));
  const byFamily = families.map((family) => {
    const rows = heldOut.filter((c) => c.case.family === family);
    const accepted = rows.filter((c) => c.runs.B2?.accepted).length;
    return { family, total: rows.length, accepted };
  });

  return (
    <div className="space-y-8">
      <header className="border-b border-line pb-6">
        <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-faint">
          Waypoint / evaluation
        </p>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">Benchmark</h1>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-muted">
          The same six held-out incidents and the same initial evidence, compared across a
          rule-only baseline, one-shot AI, and AI with bounded oracle feedback. Raw counts;
          no statistical significance is claimed.
        </p>
      </header>

      <section className="grid gap-4 md:grid-cols-3">
        {data.conditions.map((condition) => {
          const t = data.totals.by_condition?.[condition.id];
          const isB0 = condition.id === "B0";
          return (
            <div key={condition.id} className="rounded-panel border border-line bg-surface">
              <div className="flex h-9 items-center justify-between border-b border-line px-3.5">
                <h2 className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted">
                  {conditionLabel[condition.id] ?? condition.id}
                </h2>
                <span className="mono text-[10px] text-faint">
                  {isB0 ? "no patch" : `${condition.max_attempts} attempt${condition.max_attempts === 1 ? "" : "s"}`}
                </span>
              </div>
              <div className="grid grid-cols-3 gap-px bg-line">
                {isB0 ? (
                  <>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Diagnosed</p>
                      <p className="mt-1 text-lg font-semibold tabular-nums">
                        {heldOut.filter((c) => c.runs.B0?.diagnosis_correct).length}/{heldOut.length}
                      </p>
                    </div>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Patches</p>
                      <p className="mt-1 text-lg font-semibold tabular-nums">0</p>
                    </div>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Accepted</p>
                      <p className="mt-1 text-lg font-semibold tabular-nums">0</p>
                    </div>
                  </>
                ) : (
                  <>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Safe accepted</p>
                      <p className="mt-1 text-lg font-semibold tabular-nums">
                        {t?.safe_accepted ?? 0}/{heldOut.length}
                      </p>
                    </div>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Unsafe accepted</p>
                      <p className={`mt-1 text-lg font-semibold tabular-nums ${(t?.false_acceptances ?? 0) > 0 ? "text-bad" : ""}`}>
                        {t?.false_acceptances ?? 0}
                      </p>
                    </div>
                    <div className="bg-surface px-3 py-3">
                      <p className="text-[10px] uppercase tracking-wider text-faint">Errors</p>
                      <p className="mt-1 text-lg font-semibold tabular-nums">{t?.errors ?? 0}</p>
                    </div>
                  </>
                )}
              </div>
              <div className="border-t border-line px-3.5 py-2.5 text-[11px] leading-5 text-muted">
                {condition.description}
                {!isB0 && t ? ` · ${t.not_run} not run` : ""}
              </div>
            </div>
          );
        })}
      </section>

      <section className="overflow-hidden rounded-panel border border-line bg-surface">
        <div className="flex h-10 items-center justify-between border-b border-line px-4">
          <h2 className="text-[11px] font-medium uppercase tracking-wider text-muted">
            Per-case results
          </h2>
          <span className="mono text-[11px] text-faint">
            {data.model.id} · temp 0 · max {data.model.max_output_tokens?.toLocaleString()} tokens
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[980px] border-collapse text-left">
            <thead>
              <tr className="border-b border-line text-[10px] uppercase tracking-wider text-faint">
                <th className="px-4 py-2.5 font-medium">Case</th>
                <th className="px-4 py-2.5 font-medium">B0 diagnosis</th>
                <th className="px-4 py-2.5 font-medium">B1</th>
                <th className="px-4 py-2.5 font-medium">B2</th>
                <th className="px-4 py-2.5 text-right font-medium">Attempts</th>
                <th className="px-4 py-2.5 text-right font-medium">Tokens</th>
                <th className="px-4 py-2.5 text-right font-medium">Est. cost</th>
              </tr>
            </thead>
            <tbody>
              {data.cases.map((row) => {
                const b0 = row.runs.B0;
                const b1 = row.runs.B1;
                const b2 = row.runs.B2;
                const tokens = (b1?.prompt_tokens ?? 0) + (b1?.completion_tokens ?? 0) + (b2?.prompt_tokens ?? 0) + (b2?.completion_tokens ?? 0);
                const cost = (b1?.estimated_usd ?? 0) + (b2?.estimated_usd ?? 0);
                const cell = (r: typeof b1) =>
                  r ? (
                    <div>
                      <span className={`text-[11px] font-medium ${statusTone(r.status)}`}>
                        {statusLabel(r.status)}
                      </span>
                      <span className="mono mt-0.5 block whitespace-nowrap text-[10px] text-faint">
                        {r.oracle_verdict === "VERIFIED_FOR_TESTED_SCENARIOS" ? "VERIFIED" : r.oracle_verdict || "no oracle"}
                        {r.attempts ? ` · ${r.attempts} att.` : ""}
                      </span>
                    </div>
                  ) : (
                    <span className="text-[11px] text-muted">not run</span>
                  );
                return (
                  <tr key={row.case.id} className="border-b border-line/70 last:border-0">
                    <td className="px-4 py-3">
                      <Link href={`/incidents/${encodeURIComponent(row.case.id)}?cond=B2`} className="block">
                        <span className="block text-[12px] font-medium text-ink hover:text-accent">
                          {row.case.title}
                        </span>
                        <span className="mono mt-0.5 block text-[10px] text-faint">
                          {row.case.id} · {row.case.family}
                          {row.case.exposed ? " · exposed" : ""}
                        </span>
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <span className={`text-[11px] ${b0?.diagnosis_correct ? "text-ok" : "text-bad"}`}>
                        {b0?.diagnosis_correct ? "correct" : "wrong"}
                      </span>
                      <span className="mono mt-0.5 block text-[10px] text-faint">
                        {b0?.diagnosis_category ?? "—"}
                      </span>
                    </td>
                    <td className="px-4 py-3">{cell(b1)}</td>
                    <td className="px-4 py-3">{cell(b2)}</td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-xs tabular-nums text-muted">
                      {(b1?.attempts ?? 0) + (b2?.attempts ?? 0)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-[11px] tabular-nums text-muted">
                      {fmtTokens(tokens)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-[11px] tabular-nums text-muted" title={cost > 0 ? "Estimated from list prices" : "The provider did not report pricing"}>
                      {cost > 0 ? usd(cost) : "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="grid gap-4 md:grid-cols-2">
        <div className="rounded-panel border border-line bg-surface">
          <div className="border-b border-line px-4 py-2.5">
            <h2 className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted">
              By fault family (B2, held-out)
            </h2>
          </div>
          <ul className="divide-y divide-line/70">
            {byFamily.map((f) => (
              <li key={f.family} className="flex items-center justify-between px-4 py-2.5">
                <span className="text-xs text-muted">{f.family}</span>
                <span className="mono text-[11px]">
                  {f.accepted}/{f.total} accepted
                </span>
              </li>
            ))}
          </ul>
        </div>
        <div className="rounded-panel border border-line bg-surface px-4 py-3.5">
          <h2 className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted">
            Limits of this result
          </h2>
          <ul className="mt-2 space-y-1.5 text-[11px] leading-5 text-muted">
            <li>Six held-out cases: raw counts, not a generalization claim or a success rate.</li>
            <li>Zero observed false acceptances does not establish a zero real-world probability.</li>
            <li>All provider evidence is tier F; no PayPal Sandbox transaction or signed webhook was used.</li>
            <li>Costs are estimated from list prices and are labeled as estimates.</li>
            <li>B0 diagnoses but cannot patch; it is not scored as a repair system.</li>
          </ul>
        </div>
      </section>

      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4 text-[11px] text-faint">
        <span className="mono">
          corpus {data.digest.replace("sha256:", "").slice(0, 16)} · frozen {data.frozen_at.slice(0, 19)}Z
        </span>
        <span>{heldOut.length} held-out · {data.cases.length - heldOut.length} exposed reference</span>
      </div>
    </div>
  );
}
