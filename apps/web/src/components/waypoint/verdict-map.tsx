"use client";

import { useMemo } from "react";

import type { DemoExport } from "@/lib/demo";
import { compareInvariants, shortLabel, type Column } from "@/lib/evidence";
import { layoutShapes, wrapLabel } from "./canvas";
import { toneWord, useElementSize, type Tone } from "./kit";

const SEED = 21;

const toneOf = (status?: string): Tone => (status === "FAIL" ? "fail" : status === "PASS" ? "ok" : "idle");

/**
 * The invariant map: one cell per financial invariant, equal in area on purpose
 * (area carries no meaning here, tone does). The layout is fixed, so a cell
 * stays where you saw it while its tone changes with the candidate.
 */
export function VerdictMap({
  doc,
  candidate,
  selected,
  onSelect,
  differences = false,
}: {
  doc: DemoExport;
  candidate: Column;
  selected: string;
  onSelect: (id: string) => void;
  differences?: boolean;
}) {
  const rows = useMemo(() => compareInvariants(doc), [doc]);
  const [ref, { w, h }] = useElementSize<HTMLDivElement>();
  const shapes = useMemo(() => layoutShapes(rows.map(() => 1), w, h, SEED), [rows, w, h]);

  return (
    <div ref={ref} className="relative h-full min-h-[520px] w-full" data-testid="verdict-map">
      {shapes.length ? (
        <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} role="group" aria-label="Invariant map" className="absolute inset-0 select-none">
          {shapes.map((s) => {
            const row = rows[s.index];
            const inv = row[candidate === "before" ? "before" : candidate === "repair" ? "repair" : "unsafe"];
            const tone = toneOf(inv?.status);
            const isSel = selected === row.id;
            const dim = differences && !row.differs;
            const clear = rows.every((r) => r[candidate === "before" ? "before" : candidate === "repair" ? "repair" : "unsafe"]?.status !== "FAIL");
            const fill = tone === "fail" ? "var(--c-fail)" : tone === "ok" ? (isSel || clear ? "var(--cell-cream)" : "var(--cell-mid)") : "var(--cell-na)";
            const ink = tone === "fail" ? "var(--cell-cream)" : tone === "ok" ? "var(--cell-ink)" : "var(--c-faint)";
            const name = shortLabel({ id: row.id, title: inv?.title, summary: inv?.summary });
            const maxChars = Math.floor((s.radius * 1.75) / 7.6);
            const lines = maxChars >= 9 ? wrapLabel(name, maxChars, 3) : [];
            const block = 24 + lines.length * 17 + 17;
            const top = s.anchor[1] - block / 2;
            return (
              <g
                key={row.id}
                role="button"
                tabIndex={0}
                aria-pressed={isSel}
                aria-label={`${row.id} ${inv?.title ?? name}: ${toneWord(tone)}`}
                data-invariant={row.id}
                data-status={inv?.status}
                onClick={() => onSelect(row.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelect(row.id);
                  }
                }}
                className="cursor-pointer outline-none [&:focus-visible>path:first-child]:stroke-[var(--cell-cream)] [&:focus-visible>path:first-child]:stroke-[3]"
                style={{ opacity: dim ? 0.3 : 1, transition: "opacity 260ms ease" }}
              >
                <title>{`${row.id} · ${inv?.title ?? name} · ${toneWord(tone)}`}</title>
                <path
                  d={s.path}
                  className="cell-fill"
                  fill={fill}
                  stroke={isSel ? (tone === "ok" && clear ? "var(--cell-ink)" : "var(--cell-cream)") : tone === "idle" ? "var(--c-faint)" : "none"}
                  strokeWidth={isSel ? 3 : 1.5}
                  strokeDasharray={tone === "idle" && !isSel ? "2 7" : undefined}
                  strokeLinecap="round"
                />
                {s.rings.map((ring, i) => (
                  <path
                    key={i}
                    d={ring}
                    fill="none"
                    stroke={tone === "fail" ? "var(--cell-cream)" : "var(--cell-ink)"}
                    strokeWidth={1}
                    className="cell-fill"
                    opacity={tone === "fail" || isSel ? 0.34 - i * 0.03 : 0}
                    pointerEvents="none"
                  />
                ))}
                <g className="cell-fill" fill={ink} textAnchor="middle" pointerEvents="none">
                  <text x={s.anchor[0]} y={top + 18} fontSize={s.radius > 46 ? 20 : 16} fontWeight={600}>
                    {row.id}
                  </text>
                  {lines.map((line, i) => (
                    <text key={i} x={s.anchor[0]} y={top + 40 + i * 17} fontSize={14} opacity={0.92}>
                      {line}
                    </text>
                  ))}
                  <text x={s.anchor[0]} y={top + 40 + lines.length * 17 + 4} fontSize={13} opacity={0.72}>
                    {toneWord(tone)}
                  </text>
                </g>
              </g>
            );
          })}
        </svg>
      ) : null}
    </div>
  );
}
