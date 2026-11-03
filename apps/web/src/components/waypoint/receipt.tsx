"use client";

import type { CSSProperties, ReactNode } from "react";

import type { ReceiptModel } from "@/lib/evidence";
import { humanStatus, shortId } from "@/lib/showcase";
import { Barcode, Stamp, Tick } from "./kit";

export type RowKey = "capture" | "status" | "effect-1" | "effect-2" | "effect-3" | "totals" | "ticks";

/** Which of the oracle's invariants each receipt line is evidence for. */
export const ROW_INVARIANTS: Record<RowKey, string[]> = {
  capture: ["I6"],
  status: ["I8"],
  "effect-1": ["I13", "I14"],
  "effect-2": ["I13"],
  "effect-3": ["I13"],
  totals: ["I13"],
  ticks: [],
};

function Line({
  label,
  value,
  mark,
  strong,
  rowKey,
  highlight,
  selected,
  onRow,
}: {
  label: string;
  value: ReactNode;
  mark?: ReactNode;
  strong?: boolean;
  rowKey?: RowKey;
  highlight?: boolean;
  selected?: boolean;
  onRow?: (key: RowKey) => void;
}) {
  const inner = (
    <>
      <span className="shrink-0">{label}</span>
      <span className="mx-2 mb-1 min-w-3 flex-1 self-end border-b border-dotted border-current opacity-40" aria-hidden />
      <span className={`marker px-1 text-right sm:whitespace-nowrap ${strong ? "font-bold" : ""}`} data-on={highlight ? "true" : "false"}>
        {value}
      </span>
      <span className="ml-2 inline-flex w-5 justify-center">{mark}</span>
    </>
  );
  const cls = `mono flex w-full items-baseline px-5 sm:px-6 py-[7px] text-left text-[13.5px] leading-[1.35] ${selected ? "bg-black/[0.06]" : ""}`;
  if (rowKey && onRow) {
    return (
      <button type="button" data-row={rowKey} aria-pressed={selected} onClick={() => onRow(rowKey)} className={`${cls} transition-colors hover:bg-black/[0.05]`}>
        {inner}
      </button>
    );
  }
  return <div className={cls} data-row={rowKey}>{inner}</div>;
}

const Dash = () => <div className="mx-6 my-2 border-t border-dashed border-current opacity-40" aria-hidden />;

/**
 * One candidate's receipt. Two layers share the paper: the surface (what a glance
 * at the order can see) and the ledger (what the judge read). `lens` scans from
 * the top: 0 is all surface, 1 is all ledger.
 */
export function Receipt({
  model,
  amount,
  caseLabel,
  lens,
  highlight = [],
  stamped,
  selected,
  onRow,
  testId,
  className = "",
}: {
  model: ReceiptModel;
  amount: string;
  caseLabel: string;
  lens: number;
  highlight?: RowKey[];
  stamped: boolean;
  selected?: RowKey | null;
  onRow?: (key: RowKey) => void;
  testId: string;
  className?: string;
}) {
  const verified = model.verdict === "VERIFIED";
  const statusOk = model.status === "CAPTURED_VERIFIED";
  const hl = (k: RowKey) => highlight.includes(k);
  const sel = (k: RowKey) => selected === k;
  const vars = { ["--p" as string]: lens } as CSSProperties;
  const mark = (ok: boolean) => <span className={ok ? "text-[oklch(0.4_0.12_145)]" : "text-[oklch(0.5_0.21_30)]"}><Tick status={ok ? "PASS" : "FAIL"} size={15} /></span>;

  return (
    <article className={`receipt-shadow ${className}`} data-testid={testId} data-candidate={model.column} data-lens={lens.toFixed(2)} aria-label={`${model.label} receipt`}>
      <div className="receipt pb-7 pt-9" style={vars}>
        <header className="px-5 sm:px-6 text-center">
          <p className="poster-caps text-[30px]">Waypoint</p>
          <p className="mono mt-1 text-[13px] uppercase leading-snug opacity-80">{caseLabel}</p>
        </header>
        <div className="relative mt-4 flex h-[60px] items-center justify-between px-5 sm:px-6">
          <p className="poster-caps inline-block bg-[var(--sheet-ink)] px-2.5 py-1 text-[17px] leading-none text-[var(--sheet)]">{model.label}</p>
          <Stamp
            word={verified ? "Verified" : model.verdict === "UNSAFE" ? "Unsafe" : "Inconclusive"}
            tone={verified ? "ink" : "alarm"}
            on={stamped}
            className="origin-right scale-[0.72] sm:scale-[0.8]"
          />
        </div>
        <div className="mono mt-4 flex justify-between px-5 sm:px-6 text-[13.5px]">
          <span>ORDER {model.orderId ? shortId(model.orderId, 8, 4) : "n/a"}</span>
          <span className="font-bold">{amount}</span>
        </div>
        <p className="mono px-5 sm:px-6 text-[13px] opacity-70">{model.orderRole}</p>
        <Dash />

        <div className="relative grid">
          {/* Surface: everything a glance can see. */}
          <div className="[grid-area:1/1]" data-layer="surface" aria-hidden={lens > 0.5}>
            <Line label="STATUS" value={humanStatus(model.status).toUpperCase()} mark={statusOk ? mark(true) : undefined} strong />
            <Line label="CAPTURE" value="COMPLETED" mark={mark(true)} />
            <Line label="FULFILLMENT" value="DONE" mark={model.effects.length ? mark(true) : undefined} />
            <Dash />
            <Line label="PAID" value={amount} mark={mark(true)} strong />
            <p className="mono mt-5 px-5 sm:px-6 text-[13px] leading-snug opacity-60">What a glance at the order can see. Drag the lens to read the ledger underneath.</p>
          </div>

          {/* Ledger: what the judge read. Clipped from the top by the lens. */}
          <div
            className="bg-[var(--sheet)] [grid-area:1/1]"
            style={{ clipPath: `inset(0 0 ${(1 - lens) * 100}% 0)` }}
            aria-hidden={lens < 0.5}
            data-testid={`${testId}-ledger`}
            data-layer="ledger"
          >
            {model.captured ? (
              <Line label="CAPTURE" value={model.capture ?? "1 payment"} mark={mark(true)} rowKey="capture" highlight={hl("capture")} selected={sel("capture")} onRow={onRow} />
            ) : null}
            <Line
              label="STATUS"
              value={humanStatus(model.status).toUpperCase()}
              mark={mark(!model.statusFlagged)}
              rowKey="status"
              highlight={hl("status")}
              selected={sel("status")}
              onRow={onRow}
              strong
            />
            {model.effects.length ? (
              model.effects.map((fx, i) => {
                const key = `effect-${i + 1}` as RowKey;
                return (
                  <Line
                    key={i}
                    label="FULFILL"
                    value={fx.key ?? "key not exported"}
                    mark={mark(!fx.flagged)}
                    rowKey={key}
                    highlight={hl(key)}
                    selected={sel(key)}
                    onRow={onRow}
                  />
                );
              })
            ) : (
              <Line label="FULFILL" value="none" />
            )}
            <Dash />
            <Line label="CAPTURED" value={model.captured} />
            <Line label="FULFILLED" value={model.fulfilled ?? "?"} strong mark={mark(model.balanced)} rowKey="totals" highlight={hl("totals")} selected={sel("totals")} onRow={onRow} />
            <p className={`mono px-5 sm:px-6 pt-1 text-[13px] font-bold ${model.balanced ? "" : "text-[oklch(0.5_0.21_30)]"}`}>
              {model.balanced ? "THE STATEMENT BALANCES" : "THE STATEMENT DOES NOT BALANCE"}
            </p>
            <Dash />
            <div className="grid grid-cols-7 gap-1 px-5 sm:px-6" data-testid={`${testId}-ticks`}>
              {model.ticks.map((t) => (
                <div
                  key={t.id}
                  data-tick={t.id}
                  data-status={t.status}
                  title={`${t.id}: ${t.status === "PASS" ? "pass" : t.status === "FAIL" ? "fail" : "not applicable"}`}
                  className={`flex flex-col items-center py-1 ${t.status === "FAIL" ? "border-2 border-[oklch(0.5_0.21_30)] text-[oklch(0.5_0.21_30)]" : t.status === "PASS" ? "border border-dashed border-black/30" : "border border-dashed border-black/20 opacity-50"}`}
                >
                  <span className="mono text-[13px] leading-none">{t.id}</span>
                  <Tick status={t.status} size={14} />
                </div>
              ))}
            </div>
          </div>

          {/* The judge's lens line. */}
          <div className="pointer-events-none absolute inset-x-0 z-10" style={{ top: `${lens * 100}%`, opacity: lens > 0.003 && lens < 0.997 ? 1 : 0 }} aria-hidden>
            <div className="h-[3px] bg-[var(--alarm)]" />
          </div>
        </div>

        <Dash />
        <div className="px-5 sm:px-6 pt-2 text-[var(--sheet-ink)]">
          <Barcode value={model.orderId ?? model.label} />
          <p className="mono mt-2 text-center text-[13px] opacity-70">RECORDED · NOT A LIVE PAYMENT</p>
        </div>
      </div>
    </article>
  );
}
