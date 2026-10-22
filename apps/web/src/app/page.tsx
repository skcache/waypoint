import Link from "next/link";
import {
  data,
  fmtMs,
  fmtTokens,
  shortHash,
  statusLabel,
  statusTone,
} from "@/lib/m5";

export const metadata = { title: "Incidents" };

function Verdict({ value }: { value?: string }) {
  if (!value) return <span className="text-faint">—</span>;
  const tone =
    value === "VERIFIED_FOR_TESTED_SCENARIOS"
      ? "text-ok"
      : value === "UNSAFE"
        ? "text-bad"
        : "text-warn";
  return (
    <span className={`mono text-[11px] ${tone}`}>
      {value === "VERIFIED_FOR_TESTED_SCENARIOS" ? "VERIFIED" : value}
    </span>
  );
}

export default function Page() {
  const heldOut = data.cases.filter((c) => !c.case.Exposed);
  const b2 = data.totals.by_condition?.B2;
  const attempts = heldOut.reduce((n, c) => n + (c.runs.B2?.attempts ?? 0), 0);
  return (
    <div className="space-y-8">
      <section className="flex flex-col justify-between gap-5 border-b border-line pb-6 sm:flex-row sm:items-start">
        <div>
          <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-faint">
            Waypoint / review console
          </p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">Incidents</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
            Held-out payment-integration defects, the AI diagnosis and patch, and the
            independent oracle verdict a human reviews before any integration decision.
          </p>
        </div>
        <dl className="grid grid-cols-3 gap-x-8 gap-y-2 text-xs sm:text-right">
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Corpus</dt>
            <dd className="mono mt-0.5 text-ink">{data.benchmark_id}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Digest</dt>
            <dd className="mono mt-0.5 text-ink">
              {shortHash(data.digest.replace("sha256:", ""))}
            </dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wider text-faint">Evidence tier</dt>
            <dd className="mono mt-0.5 text-ink">F · scripted</dd>
          </div>
        </dl>
      </section>

      <section className="grid grid-cols-2 gap-px overflow-hidden rounded-panel border border-line bg-line sm:grid-cols-4">
        {[
          ["Held-out cases", String(heldOut.length)],
          ["Safe repairs accepted", `${b2?.safe_accepted ?? 0} / ${heldOut.length}`],
          ["Unsafe accepted", String(b2?.false_acceptances ?? 0)],
          ["Model attempts", String(attempts)],
        ].map(([label, value]) => (
          <div key={label} className="bg-surface px-4 py-4">
            <p className="text-[10px] uppercase tracking-wider text-faint">{label}</p>
            <p className="mt-1.5 text-xl font-semibold tracking-tight tabular-nums">
              {value}
            </p>
          </div>
        ))}
      </section>

      <section className="overflow-hidden rounded-panel border border-line bg-surface">
        <div className="flex h-10 items-center justify-between border-b border-line px-4">
          <h2 className="text-[11px] font-medium uppercase tracking-wider text-muted">
            Benchmark incidents
          </h2>
          <span className="mono text-[11px] text-faint">
            {data.model.id ?? "model not configured"} · base {shortHash(data.base_sha)}
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] border-collapse text-left">
            <thead>
              <tr className="border-b border-line text-[10px] uppercase tracking-wider text-faint">
                <th className="px-4 py-2.5 font-medium">Incident</th>
                <th className="px-4 py-2.5 font-medium">Family</th>
                <th className="px-4 py-2.5 font-medium">Pre-repair</th>
                <th className="px-4 py-2.5 font-medium">B2 outcome</th>
                <th className="px-4 py-2.5 text-right font-medium">Attempts</th>
                <th className="px-4 py-2.5 text-right font-medium">Prompt tokens</th>
              </tr>
            </thead>
            <tbody>
              {data.cases.map((row) => {
                const b2run = row.runs.B2 ?? row.runs.B1;
                return (
                  <tr
                    key={row.case.ID}
                    className="group border-b border-line/70 last:border-0 hover:bg-background/60"
                  >
                    <td className="px-4 py-3">
                      <Link
                        href={`/incidents/${encodeURIComponent(row.case.ID)}`}
                        className="block"
                      >
                        <span className="block text-[13px] font-medium text-ink group-hover:text-accent">
                          {row.case.Title}
                        </span>
                        <span className="mono mt-0.5 block text-[11px] text-faint">
                          {row.case.ID}
                          {row.case.Exposed ? " · exposed reference" : ""}
                        </span>
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-xs text-muted">{row.case.Family}</td>
                    <td className="px-4 py-3">
                      <Verdict value={row.pre_repair.verdict} />
                      <span className="mono mt-0.5 block text-[11px] text-faint">
                        {row.pre_repair.order_status}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      {b2run ? (
                        <>
                          <span
                            className={`text-xs font-medium ${statusTone(b2run.status)}`}
                          >
                            {statusLabel(b2run.status)}
                          </span>
                          <span className="mono mt-0.5 block whitespace-nowrap text-[11px] text-faint">
                            {b2run.oracle_verdict === "VERIFIED_FOR_TESTED_SCENARIOS" ? "VERIFIED" : b2run.oracle_verdict || "no oracle"} · {fmtMs(b2run.latency_ms)}
                          </span>
                        </>
                      ) : (
                        <span className="text-xs text-faint">not run</span>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-xs tabular-nums text-muted">
                      {b2run?.attempts ?? "—"}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-xs tabular-nums text-muted">
                      {fmtTokens(b2run?.prompt_tokens ?? 0)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <p className="max-w-3xl text-xs leading-5 text-faint">
        Six held-out cases and one exposed reference, not a statistical benchmark. Every
        provider interaction is a deterministic tier-F fixture; no PayPal transaction or
        signed webhook was used. A model patch is never merged, deployed, or treated as a
        financial authority.
      </p>
    </div>
  );
}
