"use client";

import { useState } from "react";

import type { DemoExport } from "@/lib/demo";
import { diffNotes, formatMs, totalStepMs } from "@/lib/evidence";
import { CodeViewer } from "./code-viewer";

/** Step 2's stage: the exact patch, and underneath it the sandbox run it was tested in. */
export function RepairPanel({ doc }: { doc: DemoExport }) {
  const steps = doc.verification.steps;
  const total = totalStepMs(steps);
  const [step, setStep] = useState(() => [...steps].sort((a, b) => b.duration_ms - a.duration_ms)[0]?.name ?? "");
  const chosen = steps.find((s) => s.name === step);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-5">
      <div className="h-[70svh] min-h-0 lg:h-auto lg:flex-1">
        <CodeViewer
          testId="demo-diff"
          heading="The patch, exactly as the AI generated it"
          file={doc.patch.changed_files[0] ?? "patch"}
          diff={doc.patch.diff}
          digest={doc.patch.digest}
          notes={diffNotes(doc.patch.digest)}
          fill
        />
      </div>
      <div data-testid="sandbox" className="shrink-0">
        <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
          <p className="text-[15px] font-medium">
            Tested in a disposable Docker sandbox: {(total / 1000).toFixed(1)} s, {steps.every((s) => s.passed) ? "every step passed" : "a step failed"}
          </p>
          <p className="text-[13px] text-faint">Segment width is the recorded duration.</p>
        </div>
        <div className="mt-3 flex h-9 gap-1" role="group" aria-label="Sandbox steps, widths are recorded durations">
          {steps.map((s) => (
            <button
              key={s.name}
              type="button"
              data-step={s.name}
              aria-pressed={step === s.name}
              aria-label={`${s.name}: ${formatMs(s.duration_ms)}`}
              onClick={() => setStep(s.name)}
              style={{ flexGrow: s.duration_ms, flexBasis: 0, background: !s.passed ? "var(--alarm)" : step === s.name ? "var(--c-accent)" : "var(--tile-held)" }}
              className="min-w-[8px] rounded-[10px] transition-colors hover:brightness-110"
            />
          ))}
        </div>
        <p className="mt-2 min-h-5 text-[14px] text-muted" aria-live="polite">
          {chosen ? (
            <>
              <span className="mono font-semibold text-ink">{chosen.name}</span> took {formatMs(chosen.duration_ms)} ({Math.round((chosen.duration_ms / total) * 100)}% of the run) and{" "}
              {chosen.passed ? "passed" : "failed"}. Command output stays in the local artifact directory and is not part of the public export.
            </>
          ) : null}
        </p>
      </div>
    </div>
  );
}
