"use client";

import { useMemo } from "react";

import type { DemoStep } from "@/lib/demo";
import { formatMs } from "@/lib/evidence";
import { layoutShapes, wrapLabel } from "./canvas";
import { useElementSize } from "./kit";

const SEED = 5;

/** Cells are the sandbox steps and their areas are the real recorded durations. */
export function TimeMap({ steps, selected, onSelect }: { steps: DemoStep[]; selected: string; onSelect: (name: string) => void }) {
  const [ref, { w, h }] = useElementSize<HTMLDivElement>();
  const shapes = useMemo(() => layoutShapes(steps.map((s) => Math.max(s.duration_ms, 1)), w, h, SEED), [steps, w, h]);
  const total = steps.reduce((a, s) => a + s.duration_ms, 0);

  return (
    <div ref={ref} className="relative h-full min-h-[520px] w-full" data-testid="time-map">
      {shapes.length ? (
        <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} role="group" aria-label="Sandbox time map" className="absolute inset-0 select-none">
          {shapes.map((s) => {
            const step = steps[s.index];
            const isSel = selected === step.name;
            const fill = !step.passed ? "var(--c-fail)" : isSel ? "var(--cell-cream)" : "var(--cell-mid)";
            const ink = !step.passed ? "var(--cell-cream)" : "var(--cell-ink)";
            const pct = Math.round((step.duration_ms / total) * 100);
            const roomy = s.radius > 34;
            const labelled = s.radius > 22;
            const lines = roomy ? wrapLabel(`${formatMs(step.duration_ms)} · ${pct}%`, Math.floor((s.radius * 1.7) / 7.6), 2) : [];
            const top = s.anchor[1] - (roomy ? 30 : 8);
            return (
              <g
                key={step.name}
                role="button"
                tabIndex={0}
                aria-pressed={isSel}
                aria-label={`${step.name}: ${formatMs(step.duration_ms)}, ${step.passed ? "passed" : "failed"}`}
                data-step={step.name}
                onClick={() => onSelect(step.name)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelect(step.name);
                  }
                }}
                className="cursor-pointer outline-none [&:focus-visible>path:first-child]:stroke-[var(--cell-cream)] [&:focus-visible>path:first-child]:stroke-[3]"
              >
                <title>{`${step.name} · ${formatMs(step.duration_ms)}`}</title>
                <path d={s.path} className="cell-fill" fill={fill} stroke={isSel ? "var(--cell-cream)" : "none"} strokeWidth={3} />
                {s.rings.map((ring, i) => (
                  <path key={i} d={ring} fill="none" stroke="var(--cell-ink)" strokeWidth={1} opacity={isSel ? 0.28 - i * 0.025 : 0} className="cell-fill" pointerEvents="none" />
                ))}
                <g fill={ink} textAnchor="middle" pointerEvents="none" display={labelled ? undefined : "none"}>
                  <text x={s.anchor[0]} y={top + 18} fontSize={roomy ? 18 : 14} fontWeight={600} fontFamily="var(--font-mono)">
                    {step.name}
                  </text>
                  {lines.map((line, i) => (
                    <text key={i} x={s.anchor[0]} y={top + 40 + i * 17} fontSize={14} opacity={0.85}>
                      {line}
                    </text>
                  ))}
                </g>
              </g>
            );
          })}
        </svg>
      ) : null}
    </div>
  );
}
