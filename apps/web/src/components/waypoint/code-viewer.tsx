"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowsInLineHorizontal, ArrowsOutLineHorizontal } from "@phosphor-icons/react";

import { diffStats, parseDiff, tokenizeLine, type TokenKind } from "@/lib/diff";
import type { DiffNote } from "@/lib/evidence";
import { CopyChip } from "./kit";

const TOKEN_COLOR: Record<TokenKind, string | undefined> = {
  keyword: "var(--t-keyword)",
  string: "var(--t-string)",
  comment: "var(--t-comment)",
  number: "var(--t-number)",
  type: "var(--t-type)",
  sql: "var(--t-sql)",
  plain: undefined,
};

function Code({ text }: { text: string }) {
  return (
    <>
      {tokenizeLine(text).map((t, i) => (
        <span
          key={i}
          style={{ color: TOKEN_COLOR[t.kind] }}
          className={t.kind === "keyword" ? "font-medium" : t.kind === "comment" ? "italic" : undefined}
        >
          {t.text}
        </span>
      ))}
    </>
  );
}

export function CodeViewer({
  file,
  diff,
  digest,
  notes = [],
  testId,
  heading,
  fill = false,
  maxHeight,
}: {
  file: string;
  diff: string;
  digest?: string;
  notes?: DiffNote[];
  testId?: string;
  heading?: string;
  /** Fill the parent height and scroll inside it, like a sheet. */
  fill?: boolean;
  /** A CSS max-height for the scrolling code area when not filling. */
  maxHeight?: string;
}) {
  const { lines, hunks } = useMemo(() => parseDiff(diff), [diff]);
  const stats = useMemo(() => diffStats(diff), [diff]);
  const [wrap, setWrap] = useState(true);
  const [flash, setFlash] = useState<number | null>(null);
  const [activeNote, setActiveNote] = useState<number | null>(null);
  const rowRefs = useRef<Record<number, HTMLDivElement | null>>({});

  // Resolve each note to the first diff line containing its match text.
  const placed = useMemo(
    () =>
      notes
        .map((note, index) => ({ ...note, index, line: lines.findIndex((l) => l.kind !== "meta" && l.kind !== "hunk" && l.text.includes(note.match)) }))
        .filter((n) => n.line >= 0),
    [notes, lines],
  );
  const noteAt = useMemo(() => new Map(placed.map((n) => [n.line, n])), [placed]);

  useEffect(() => {
    if (flash === null) return;
    const id = window.setTimeout(() => setFlash(null), 1500);
    return () => window.clearTimeout(id);
  }, [flash]);

  function jump(lineIndex: number, noteIndex: number) {
    setActiveNote(noteIndex);
    setFlash(lineIndex);
    rowRefs.current[lineIndex]?.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  return (
    <div className={`paper overflow-hidden rounded-xl border border-line bg-background ${fill ? "flex h-full min-h-0 flex-col" : ""}`} data-testid={testId}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-line px-4 py-3">
        <div className="min-w-0">
          {heading ? <p className="mb-0.5 text-[15px] font-medium text-faint">{heading}</p> : null}
          <p className="mono truncate text-[15px] text-ink" title={file}>{file}</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[15px] font-medium">
            <span className="text-ok">+{stats.added}</span> <span className="text-bad">−{stats.removed}</span>
          </span>
          {digest ? <CopyChip value={digest} label={`${digest.slice(0, 15)}…`} /> : null}
          <button
            type="button"
            aria-pressed={wrap}
            onClick={() => setWrap((v) => !v)}
            className="inline-flex min-h-11 items-center gap-2 rounded px-3 py-1 text-[14px] text-muted max-sm:hidden transition-colors hover:bg-sunk hover:text-ink"
          >
            {wrap ? <ArrowsOutLineHorizontal size={14} aria-hidden /> : <ArrowsInLineHorizontal size={14} aria-hidden />}
            {wrap ? "Unwrap" : "Wrap"}
          </button>
        </div>
      </div>

      {placed.length ? (
        <ol className="flex flex-wrap items-center gap-2 border-b border-line bg-surface px-4 py-3" aria-label="Notes on the patch">
          {placed.map((n) => (
            <li key={n.index}>
              <button
                type="button"
                onClick={() => jump(n.line, n.index)}
                aria-pressed={activeNote === n.index}
                className={`grid h-11 w-11 place-items-center rounded border text-[13px] font-medium transition-colors ${
                  activeNote === n.index ? "border-accent bg-accent text-on-accent" : "border-line-strong text-muted hover:border-accent hover:text-ink"
                }`}
                aria-label={`Note ${n.index + 1}: ${n.note}`}
              >
                {n.index + 1}
              </button>
            </li>
          ))}
          <li className="ml-1 flex min-h-9 min-w-0 flex-1 basis-60 items-center text-[15px] text-muted" aria-live="polite">
            {activeNote !== null && placed.find((n) => n.index === activeNote)
              ? placed.find((n) => n.index === activeNote)?.note
              : "Select a number to see the line."}
          </li>
        </ol>
      ) : null}

      <div className={fill ? "min-h-0 flex-1 overflow-auto" : "overflow-auto"} style={fill ? undefined : { maxHeight: maxHeight ?? "560px" }}>
        <div className={`mono text-[14px] leading-[1.7] max-sm:min-w-max ${wrap ? "" : "min-w-max"}`}>
          {lines.map((line, i) => {
            if (line.kind === "meta") return null;
            if (line.kind === "hunk") {
              const hunk = hunks[line.hunk];
              return (
                <div key={i} className="flex items-center gap-3 whitespace-nowrap border-y border-line bg-sunk px-4 py-2 text-[13px] text-faint">
                  <span className="shrink-0 text-accent">{line.text.replace(/ @@.*$/, " @@")}</span>
                  {hunk?.context ? <span className="truncate text-muted">{hunk.context}</span> : null}
                </div>
              );
            }
            const add = line.kind === "add";
            const del = line.kind === "del";
            const note = noteAt.get(i);
            return (
              <div
                key={i}
                ref={(el) => {
                  rowRefs.current[i] = el;
                }}
                className={`grid grid-cols-[2.6rem_1.4rem_1fr] sm:grid-cols-[2.6rem_2.6rem_1.4rem_1fr] ${add ? "bg-ok-soft" : del ? "bg-bad-soft" : ""} ${flash === i ? "line-pulse" : ""}`}
              >
                <span className="select-none px-2 text-right text-[13px] leading-[1.75] text-faint max-sm:hidden">{line.oldNo ?? ""}</span>
                <span className="select-none px-2 text-right text-[13px] leading-[1.75] text-faint">{line.newNo ?? ""}</span>
                <span className={`select-none text-center ${add ? "text-ok" : del ? "text-bad" : "text-faint"}`} aria-hidden>
                  {add ? "+" : del ? "−" : ""}
                </span>
                <span className={`relative min-w-0 pr-10 ${wrap ? "whitespace-pre sm:whitespace-pre-wrap sm:break-words" : "whitespace-pre"}`} style={{ tabSize: 4 }}>
                  <Code text={line.text || " "} />
                  {note ? (
                    <button
                      type="button"
                      onClick={() => jump(i, note.index)}
                      aria-label={`Note ${note.index + 1}`}
                      className="absolute right-2 top-0.5 grid h-7 w-7 place-items-center rounded border border-accent bg-background text-[13px] font-medium text-accent"
                    >
                      {note.index + 1}
                    </button>
                  ) : null}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
