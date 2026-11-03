"use client";

import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";

import { cleanEffect, type RuleTile } from "@/lib/evidence";
import { humanStatus } from "@/lib/showcase";
import { stripLayout } from "@/lib/treemap";

type Variant = "big" | "wide" | "mid" | "small";

const WORD: Record<string, string> = { PASS: "Held", FAIL: "Failed", NOT_APPLICABLE: "Not applicable" };
export const stateWord = (status: string) => WORD[status] ?? status.toLowerCase();

/** What the oracle's counterexample says, in the order a reader needs it. */
export function counterexampleFacts(cx: RuleTile["counterexample"]): string[] {
  if (!cx) return [];
  const out: string[] = [];
  if (cx.final_status) out.push(`Final status: ${humanStatus(cx.final_status)}`);
  if (cx.effect_count !== undefined) out.push(`${cx.effect_count} fulfillment effects`);
  if (cx.effects?.length) out.push(cx.effects.map(cleanEffect).join(", "));
  if (cx.event_id) out.push(`Event ${cx.event_id}`);
  return out;
}

/** Concentric contour lines inside a failed tile: inset shadows, thin lines over gaps of the tile's own colour. */
function rings(count: number, gap: number): string {
  const line = "color-mix(in oklch, black 34%, var(--tile-fail))";
  const parts: string[] = [];
  for (let k = 0; k < count; k++) {
    parts.push(`inset 0 0 0 ${k * gap + 1}px ${line}`);
    parts.push(`inset 0 0 0 ${(k + 1) * gap}px var(--tile-fail)`);
  }
  parts.push(`inset 0 0 0 ${count * gap + 1}px ${line}`);
  return parts.join(", ");
}

const RINGS: Record<Variant, [number, number]> = { big: [3, 8], wide: [2, 8], mid: [1, 8], small: [0, 0] };
const PAD: Record<Variant, string> = { big: "p-9", wide: "p-7", mid: "p-4", small: "p-3" };

function tileStyle(status: string, lit: boolean, variant: Variant): CSSProperties {
  if (status === "FAIL") {
    const [count, gap] = RINGS[variant];
    return { background: "var(--tile-fail)", color: "var(--tile-fail-ink)", boxShadow: count ? rings(count, gap) : undefined };
  }
  if (status === "PASS") {
    return lit
      ? { background: "var(--tile-lit)", color: "var(--tile-lit-ink)" }
      : { background: "var(--tile-held)", color: "var(--tile-held-ink)" };
  }
  return { background: "transparent", color: "var(--c-faint)", border: "1.5px dotted var(--tile-na-line)" };
}

function variantFor(w: number, h: number): Variant {
  if (w >= 300 && h < 250 && w / h >= 1.5) return "wide";
  if (w >= 230 && h >= 250) return "big";
  if (w >= 140 && h >= 110) return "mid";
  return "small";
}

const clamp = (lines: number): CSSProperties => ({ display: "-webkit-box", WebkitLineClamp: lines, WebkitBoxOrient: "vertical", overflow: "hidden", overflowWrap: "anywhere" });

/**
 * The board: one tile per rule the checker tests. Area is proportional to the
 * tile's weight (a failed rule gets four times the space of one that held), the
 * layout fills the stage exactly, and tiles glide to their new places when the
 * candidate changes. An accepted candidate's held rules are lit.
 */
export function Board({
  tiles,
  lit,
  selected,
  onSelect,
  label,
}: {
  tiles: RuleTile[];
  lit: boolean;
  selected: string | null;
  onSelect: (id: string | null) => void;
  label: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 960, h: 620 });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) setSize((s) => (Math.abs(s.w - width) > 1 || Math.abs(s.h - height) > 1 ? { w: width, h: height } : s));
    });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  const rects = useMemo(() => stripLayout(tiles.map((t) => t.weight), size.w / size.h), [tiles, size]);

  return (
    <div ref={ref} className="relative size-full" role="group" aria-label={label} data-testid="board">
      {tiles.map((t, i) => {
        const r = rects[i];
        if (!r) return null;
        const w = r.w * size.w;
        const h = r.h * size.h;
        const variant = variantFor(w, h);
        const facts = variant === "big" || variant === "wide" ? counterexampleFacts(t.counterexample) : [];
        const word = stateWord(t.status);
        // Lines of title that fit once the id, the state word and the padding are paid for.
        const titleLines = variant === "mid" ? Math.min(5, Math.floor((h - 96) / 16)) : 0;
        return (
          <div
            key={t.id}
            className="absolute p-1 transition-[left,top,width,height] duration-500 ease-[cubic-bezier(0.22,1,0.36,1)]"
            style={{ left: `${r.x * 100}%`, top: `${r.y * 100}%`, width: `${r.w * 100}%`, height: `${r.h * 100}%` }}
          >
            <button
              type="button"
              data-tile={t.id}
              data-status={t.status}
              data-variant={variant}
              aria-pressed={selected === t.id}
              aria-label={`${t.id}, ${t.title}: ${word}`}
              onClick={() => onSelect(selected === t.id ? null : t.id)}
              style={tileStyle(t.status, lit, variant)}
              className={`flex size-full min-w-0 overflow-hidden rounded-[16px] text-left transition-[filter,outline-color] duration-200 hover:brightness-110 ${PAD[variant]} ${
                variant === "wide" ? "flex-row items-center gap-6" : "flex-col justify-between"
              } ${selected === t.id ? "outline outline-2 outline-offset-2 outline-accent" : "outline outline-2 outline-offset-2 outline-transparent"}`}
            >
              {variant === "wide" ? (
                <>
                  <span className="numeral shrink-0 text-[56px]">{t.id}</span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-[13px] font-medium">{word}</span>
                    <span className="block text-[20px] font-medium leading-[1.2]" style={clamp(2)}>{t.title}</span>
                    {facts.length ? <span className="mt-1 block text-[14px] leading-snug" style={clamp(2)}>{facts.join(" · ")}</span> : null}
                  </span>
                </>
              ) : (
                <>
                  <span className="flex items-baseline justify-between gap-2">
                    <span className={`numeral ${variant === "big" ? "text-[56px]" : variant === "mid" ? "text-[28px]" : "text-[20px]"}`}>{t.id}</span>
                    {variant === "big" ? <span className="text-[14px] font-medium">{word}</span> : null}
                  </span>
                  {variant === "big" ? (
                    <span className="block text-[20px] font-medium leading-[1.25] [overflow-wrap:anywhere]">{t.title}</span>
                  ) : variant === "mid" && titleLines >= 2 ? (
                    <span className="block min-h-0 text-[13px] font-medium leading-[1.25]" style={clamp(titleLines)}>{t.title}</span>
                  ) : null}
                  <span className="block">
                    {facts.length ? (
                      <span className="mb-2 block space-y-1 text-[14px] leading-snug">
                        {facts.map((f) => (
                          <span key={f} className="block">{f}</span>
                        ))}
                      </span>
                    ) : null}
                    {variant !== "big" ? <span className="block text-[13px] font-medium leading-tight">{variant === "small" && t.status === "NOT_APPLICABLE" ? "N/A" : word}</span> : null}
                  </span>
                </>
              )}
            </button>
          </div>
        );
      })}
    </div>
  );
}
