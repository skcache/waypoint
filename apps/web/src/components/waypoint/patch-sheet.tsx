"use client";

import { useState } from "react";

import type { DemoExport } from "@/lib/demo";
import { diffNotes } from "@/lib/evidence";
import { CodeViewer } from "./code-viewer";
import type { Candidate } from "./types";

/** Code on a cream sheet: the canvas for the Fix act and for the Judge's code view. */
export function PatchSheet({ doc, candidate, defaultBoth = false }: { doc: DemoExport; candidate: Candidate; defaultBoth?: boolean }) {
  const [both, setBoth] = useState(defaultBoth);
  const hasUnsafe = Boolean(doc.unsafe.diff);
  const repair = (
    <CodeViewer
      fill
      testId="demo-diff"
      heading="AI repair · exactly as generated"
      file={doc.patch.changed_files[0] ?? "patch"}
      diff={doc.patch.diff}
      digest={doc.patch.digest}
      notes={diffNotes(doc.patch.digest)}
    />
  );
  const unsafe = (
    <CodeViewer
      fill
      testId="demo-unsafe-diff"
      heading="Seeded unsafe patch"
      file={doc.unsafe.changed_files?.[0] ?? "patch"}
      diff={doc.unsafe.diff ?? ""}
      digest={doc.unsafe.diff_digest}
      notes={diffNotes(doc.unsafe.diff_digest)}
    />
  );

  if (candidate === "before") {
    return (
      <div className="paper flex h-full min-h-[420px] items-center rounded-[20px] p-8" data-testid="demo-code">
        <p className="max-w-[40ch] text-xl leading-[1.5] text-ink">
          No patch: this is the incident as shipped. The lines either patch removes are the original code.
        </p>
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-[520px] flex-col gap-3" data-testid="demo-code">
      {hasUnsafe && doc.patch.diff ? (
        <div className="flex shrink-0 justify-end">
          <button
            type="button"
            aria-pressed={both}
            onClick={() => setBoth((v) => !v)}
            className="inline-flex min-h-11 items-center rounded-lg border border-line-strong px-4 text-[15px] text-ink transition-colors hover:bg-cell-dark aria-pressed:bg-cell-dark"
          >
            {both ? "Show one patch" : "Compare both patches"}
          </button>
        </div>
      ) : null}
      <div className={`grid min-h-0 flex-1 gap-3 ${both ? "xl:grid-cols-2" : ""}`}>
        {both ? (
          <>
            {repair}
            {unsafe}
          </>
        ) : candidate === "unsafe" && hasUnsafe ? (
          unsafe
        ) : (
          repair
        )}
      </div>
    </div>
  );
}
