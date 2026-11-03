import Link from "next/link";
import { data, fmtTokens, statusLabel, statusTone, usd } from "@/lib/m5";

export const metadata = { title: "Benchmark" };

const conditionLabel: Record<string, string> = {
  B0: "B0 · Rules only",
  B1: "B1 · One AI attempt",
  B2: "B2 · AI with checker feedback",
};

export default function Page() {
  const heldOut = data.cases.filter((c) => !c.case.exposed);
  const families = Array.from(new Set(heldOut.map((c) => c.case.family)));
  const byFamily = families.map((family) => {
    const rows = heldOut.filter((c) => c.case.family === family);
    const accepted = rows.filter((c) => c.runs.B2?.accepted).length;
    return { family, total: rows.length, accepted };
  });
  const describe = (id: string, maxAttempts: number) =>
    id === "B0"
      ? "Fixed rules read the incident data. They find a cause. They do not write a patch."
      : id === "B1"
        ? "One diagnosis and one patch. No checker feedback."
        : `The same first attempt. Then a retry with feedback from the checker. Maximum ${maxAttempts} attempts.`;

  return (
    <div className="space-y-16">
      <header className="max-w-[64ch]">
        <h1 className="text-[32px] font-medium leading-[1.15]">Benchmark</h1>
        <p className="mt-4 text-[15px] leading-[1.6] text-muted">
          This page compares 3 methods on the same {heldOut.length} held-out incidents. The numbers are raw counts. They are not a statistical result.
        </p>
      </header>

      <section className="grid gap-x-10 gap-y-12 border-t border-line pt-10 lg:grid-cols-[1fr_1fr_1.7fr]" aria-label="Results by method">
        {data.conditions.map((condition) => {
          const t = data.totals.by_condition?.[condition.id];
          const isB0 = condition.id === "B0";
          const lead = condition.id === "B2";
          const diagnosed = heldOut.filter((c) => c.runs.B0?.diagnosis_correct).length;
          return (
            <div key={condition.id} data-condition={condition.id}>
              <h2 className="text-[14px] font-medium text-muted">{conditionLabel[condition.id] ?? condition.id}</h2>
              <p className="mt-5 flex items-baseline gap-3">
                <span className={`numeral ${lead ? "text-[104px]" : "text-[64px]"}`}>{isB0 ? diagnosed : t?.safe_accepted ?? 0}</span>
                <span className="text-[20px] text-faint">/ {heldOut.length}</span>
              </p>
              <p className="mt-3 text-[15px]">{isB0 ? "causes found" : "safe patches accepted"}</p>
              <dl className="mt-5 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-[14px]">
                {isB0 ? (
                  <>
                    <dt className="text-faint">Patches written</dt>
                    <dd className="tabular-nums">0</dd>
                  </>
                ) : (
                  <>
                    <dt className="text-faint">Unsafe patches accepted</dt>
                    <dd className={`tabular-nums ${(t?.false_acceptances ?? 0) > 0 ? "text-bad" : ""}`}>{t?.false_acceptances ?? 0}</dd>
                    <dt className="text-faint">Errors</dt>
                    <dd className="tabular-nums">{t?.errors ?? 0}</dd>
                    <dt className="text-faint">Not run</dt>
                    <dd className="tabular-nums">{t?.not_run ?? 0}</dd>
                  </>
                )}
              </dl>
              <p className="mt-5 max-w-[40ch] text-[14px] leading-[1.55] text-muted">{describe(condition.id, condition.max_attempts)}</p>
            </div>
          );
        })}
      </section>

      <section aria-label="Results by case">
        <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
          <h2 className="text-[20px] font-medium">Results by case</h2>
          <span className="mono text-[13px] text-faint">
            {data.model.id} · temperature 0 · max {data.model.max_output_tokens?.toLocaleString()} tokens
          </span>
        </div>
        <div className="mt-5 overflow-x-auto">
          <table className="w-full min-w-[980px] border-collapse text-left">
            <thead>
              <tr className="border-b border-line-strong text-[13px] text-faint">
                <th className="py-3 pr-4 font-medium">Case</th>
                <th className="px-4 py-3 font-medium">B0 cause</th>
                <th className="px-4 py-3 font-medium">B1</th>
                <th className="px-4 py-3 font-medium">B2</th>
                <th className="px-4 py-3 text-right font-medium">Attempts</th>
                <th className="px-4 py-3 text-right font-medium">Tokens</th>
                <th className="py-3 pl-4 text-right font-medium">Cost</th>
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
                      <span className={`text-[14px] font-medium ${statusTone(r.status)}`}>{statusLabel(r.status)}</span>
                      <span className="mono mt-1 block whitespace-nowrap text-[13px] text-faint">
                        {r.oracle_verdict === "VERIFIED_FOR_TESTED_SCENARIOS" ? "VERIFIED" : r.oracle_verdict || "no check"}
                        {r.attempts ? ` · ${r.attempts} attempt${r.attempts === 1 ? "" : "s"}` : ""}
                      </span>
                    </div>
                  ) : (
                    <span className="text-[14px] text-muted">not run</span>
                  );
                return (
                  <tr key={row.case.id} className="border-b border-line last:border-0">
                    <td className="py-4 pr-4">
                      <Link href={`/incidents/${encodeURIComponent(row.case.id)}?cond=B2`} className="block">
                        <span className="block text-[15px] font-medium text-ink hover:text-accent">{row.case.title}</span>
                        <span className="mono mt-1 block text-[13px] text-faint">
                          {row.case.id} · {row.case.family}
                          {row.case.exposed ? " · exposed" : ""}
                        </span>
                      </Link>
                    </td>
                    <td className="px-4 py-4">
                      <span className={`text-[14px] ${b0?.diagnosis_correct ? "text-ok" : "text-bad"}`}>{b0?.diagnosis_correct ? "Correct" : "Wrong"}</span>
                      <span className="mono mt-1 block text-[13px] text-faint">{b0?.diagnosis_category ?? "—"}</span>
                    </td>
                    <td className="px-4 py-4">{cell(b1)}</td>
                    <td className="px-4 py-4">{cell(b2)}</td>
                    <td className="whitespace-nowrap px-4 py-4 text-right text-[14px] tabular-nums text-muted">{(b1?.attempts ?? 0) + (b2?.attempts ?? 0)}</td>
                    <td className="whitespace-nowrap px-4 py-4 text-right text-[14px] tabular-nums text-muted">{fmtTokens(tokens)}</td>
                    <td className="whitespace-nowrap py-4 pl-4 text-right text-[14px] tabular-nums text-muted" title={cost > 0 ? "Estimate from list prices" : "The provider reported no price"}>
                      {cost > 0 ? usd(cost) : "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="grid gap-x-10 gap-y-10 border-t border-line pt-10 md:grid-cols-2">
        <div>
          <h2 className="text-[20px] font-medium">B2 by fault family</h2>
          <ul className="mt-5 divide-y divide-line border-y border-line">
            {byFamily.map((f) => (
              <li key={f.family} className="flex items-center justify-between py-3">
                <span className="text-[14px] text-muted">{f.family}</span>
                <span className="mono text-[14px]">
                  {f.accepted} of {f.total} accepted
                </span>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <h2 className="text-[20px] font-medium">Limits</h2>
          <ul className="mt-5 space-y-3 text-[14px] leading-[1.55] text-muted">
            <li>The test has {heldOut.length} held-out cases. The numbers are counts. They are not a success rate.</li>
            <li>No false acceptance was seen. This does not prove that the real risk is zero.</li>
            <li>All provider evidence is tier F. The test did not use a PayPal Sandbox payment or a signed webhook.</li>
            <li>Costs are estimates from list prices.</li>
            <li>B0 finds causes but cannot write patches. It is not a repair system.</li>
          </ul>
        </div>
      </section>

      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-5 text-[13px] text-faint">
        <span className="mono">
          corpus {data.digest.replace("sha256:", "").slice(0, 16)} · frozen {data.frozen_at.slice(0, 19)}Z
        </span>
        <span>
          {heldOut.length} held-out · {data.cases.length - heldOut.length} exposed reference
        </span>
      </div>
    </div>
  );
}
