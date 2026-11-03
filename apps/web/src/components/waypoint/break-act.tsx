"use client";

import { useMemo, useState } from "react";

import type { DemoExport, DemoInvariant } from "@/lib/demo";
import { effectCount, ledgerAutopsy, orderIdentities, type LedgerRow } from "@/lib/evidence";
import { humanStatus, money } from "@/lib/showcase";
import { ActHeading, Container, Fact, StatusMark } from "./ui";

type Selection = { kind: "row"; key: string } | { kind: "inv"; id: string };

export function BreakAct({ doc }: { doc: DemoExport }) {
  const rows = useMemo(() => ledgerAutopsy(doc.pre_repair), [doc.pre_repair]);
  const ids = orderIdentities(doc);
  const count = effectCount(doc.pre_repair);
  const captured = doc.pre_repair.provider_capture_id ? 1 : 0;
  const firstFail = rows.find((r) => r.state === "fail");
  const [sel, setSel] = useState<Selection>({ kind: "row", key: (firstFail ?? rows[0])?.key ?? "status" });
  const observations = doc.incident.observations;
  const webhookRows = observations.some((o) => /webhook/i.test(o.method));

  const selectedRow = sel.kind === "row" ? rows.find((r) => r.key === sel.key) : undefined;
  const detailInvariants: DemoInvariant[] =
    sel.kind === "inv"
      ? doc.pre_repair.invariants.filter((i) => i.id === sel.id)
      : (selectedRow?.invariantIds ?? [])
          .map((id) => doc.pre_repair.invariants.find((i) => i.id === id))
          .filter((i): i is DemoInvariant => Boolean(i));
  const balanced = count !== null && count === captured;

  return (
    <section id="break" className="scroll-mt-14 border-b border-line">
      <Container className="py-20 sm:py-28">
        <ActHeading
          title={
            <>
              One payment captured.
              <br />
              {count ?? "More"} fulfillments recorded.
            </>
          }
        >
          After the run, the judge read the merchant&rsquo;s ledger for this order. Select any entry to see why it
          flagged it.
        </ActHeading>

        <div className="mt-12 grid gap-6 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)] lg:gap-10">
          <div>
            <div className="rounded-panel bg-surface">
              <div className="flex items-baseline justify-between gap-4 px-5 pt-5 pb-3">
                <p className="text-lg font-semibold tracking-[-0.01em]">Ledger statement</p>
                <p className="text-lg tabular-nums">{money(doc.incident.total_minor, doc.incident.currency)}</p>
              </div>
              <p className="px-5 pb-3 text-[15px] text-muted">
                Incident order <span className="mono">{ids.incident}</span>
              </p>
              <ul aria-label="Ledger entries" className="border-t border-line">
                {rows.map((row) => (
                  <EntryRow key={row.key} row={row} on={sel.kind === "row" && sel.key === row.key} onClick={() => setSel({ kind: "row", key: row.key })} />
                ))}
              </ul>
              <div className="double-rule mx-5" aria-hidden />
              <dl className="grid grid-cols-2 gap-4 px-5 py-5">
                <div>
                  <dt className="label">Captured</dt>
                  <dd className="mt-1 text-4xl font-semibold tabular-nums tracking-[-0.02em]">{captured}</dd>
                </div>
                <div>
                  <dt className="label">Fulfilled</dt>
                  <dd className={`mt-1 text-4xl font-semibold tabular-nums tracking-[-0.02em] ${balanced ? "text-ok" : "text-bad"}`}>{count ?? "?"}</dd>
                </div>
              </dl>
              <p className="px-5 pb-5 text-[15px] text-muted">
                {balanced ? "Captured and fulfilled counts match." : "One capture cannot support more than one fulfillment, so the statement does not balance."}{" "}
                The entries are listed, not timed: the export records what the ledger held at the end, not when each row arrived.
              </p>
            </div>

            <p className="label mt-8">All {doc.pre_repair.invariants.length} invariants on this order</p>
            <div className="mt-3 grid grid-cols-7 gap-2 sm:grid-cols-14" role="group" aria-label="Invariants">
              {doc.pre_repair.invariants.map((inv) => {
                const fail = inv.status === "FAIL";
                const na = inv.status === "NOT_APPLICABLE";
                const on = sel.kind === "inv" && sel.id === inv.id;
                return (
                  <button
                    key={inv.id}
                    type="button"
                    aria-pressed={on}
                    onClick={() => setSel({ kind: "inv", id: inv.id })}
                    title={inv.summary}
                    className={`flex h-11 items-center justify-center rounded text-[13px] font-semibold transition-colors ${
                      fail ? "bg-bad-soft text-bad" : na ? "bg-surface text-faint" : "bg-surface text-muted hover:bg-sunk"
                    } ${on ? "outline-2 outline-offset-1 outline-accent" : ""}`}
                  >
                    <span className="sr-only">{inv.status}</span>
                    {inv.id}
                  </button>
                );
              })}
            </div>
          </div>

          <aside className="h-fit lg:sticky lg:top-24" aria-live="polite" data-testid="break-detail">
            <h3 className="text-xl font-semibold tracking-[-0.01em]">
              {sel.kind === "row" ? selectedRow?.title : `Invariant ${sel.id}`}
            </h3>
            {selectedRow ? <p className="mono mt-1 break-all text-lg text-ink">{selectedRow.value}</p> : null}
            {detailInvariants.map((inv) => (
              <div key={inv.id} className="mt-5 border-t border-line pt-4">
                <p className="flex items-center gap-3">
                  <span className="text-lg font-semibold">{inv.id}</span>
                  <StatusMark status={inv.status} />
                </p>
                <p className="mt-2 text-[17px] leading-[1.55]">{inv.summary}</p>
              </div>
            ))}
            <dl className="mt-4 divide-y divide-line border-t border-line">
              {(selectedRow?.facts ?? []).map((f) => (
                <Fact key={f.label} label={f.label}>
                  <span className="mono break-all">{f.value.replace(/^[A-Z_]+$/, (v) => humanStatus(v))}</span>
                </Fact>
              ))}
              <Fact label="Order">
                <span className="mono break-all">{ids.incident}</span>
                <span className="mt-0.5 block text-sm text-faint">the incident order, not the verification order</span>
              </Fact>
            </dl>
          </aside>
        </div>

        {!webhookRows ? (
          <details className="group mt-12 max-w-[760px] border-y border-line">
            <summary className="flex min-h-12 items-center py-3 text-base font-medium">What this evidence cannot show</summary>
            <p className="pb-4 text-base leading-[1.6] text-muted">
              The export has no webhook-delivery rows, and its {observations.length} provider calls belong to the later
              verification order. So how the second effect arrived, and when, was not observed. The AI&rsquo;s
              explanation in the next section is a hypothesis about that, not a recording of it.
            </p>
          </details>
        ) : null}
      </Container>
    </section>
  );
}

function EntryRow({ row, on, onClick }: { row: LedgerRow; on: boolean; onClick: () => void }) {
  const fail = row.state === "fail";
  return (
    <li className="border-b border-line last:border-0">
      <button
        type="button"
        aria-pressed={on}
        onClick={onClick}
        data-testid="ledger-row"
        className={`grid min-h-[68px] w-full grid-cols-[1fr_auto] items-center gap-3 px-5 py-3 text-left transition-colors hover:bg-sunk ${on ? "bg-sunk" : ""}`}
      >
        <span className="min-w-0">
          <span className="block text-[15px] text-faint">{row.title}</span>
          <span className="mono block truncate text-[17px] text-ink">{row.value}</span>
        </span>
        <span className="flex flex-wrap items-center justify-end gap-2">
          <span className="text-[13px] text-faint">{row.invariantIds.join(" · ")}</span>
          <StatusMark status={fail ? "FAIL" : "PASS"} />
        </span>
      </button>
    </li>
  );
}
