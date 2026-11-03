"use client";

import type { DemoExport } from "@/lib/demo";
import { diffNotes, firstSentence } from "@/lib/evidence";
import { modelLabel, modelNote, shortId, utcTimestamp } from "@/lib/showcase";
import { CodeViewer } from "./code-viewer";
import { Reveal } from "./kit";

export function FixScene({ doc }: { doc: DemoExport }) {
  const d = doc.diagnosis;
  const files = doc.patch.changed_files;
  const seconds = (ms?: number) => (ms ? `${(ms / 1000).toFixed(1)} s` : "–");
  const added = doc.patch.diff.split("\n").filter((l) => l.startsWith("+") && !l.startsWith("+++")).length;
  const removed = doc.patch.diff.split("\n").filter((l) => l.startsWith("-") && !l.startsWith("---")).length;

  return (
    <section id="fix" className="field-ink" data-scene="fix">
      <div id="fix-intro" className="mx-auto w-full max-w-[1440px] px-5 py-24 sm:px-8 lg:px-12 lg:py-36">
        <Reveal>
          <h2 className="poster text-[clamp(2.8rem,6.6vw,6.2rem)]">
            The AI rewrote
            <br />
            <span className="text-clear">{files.length === 1 ? "one file." : `${files.length} files.`}</span>
          </h2>
          <p className="mt-8 max-w-[44ch] text-[clamp(1.15rem,1.8vw,1.45rem)] leading-[1.4] text-muted">
            This is the model&rsquo;s recorded diagnosis and the exact patch it produced: +{added} lines, −{removed} lines. Read both before you see how the judge scored
            it.
          </p>
        </Reveal>

        <div className="mt-16 grid grid-cols-[minmax(0,1fr)] gap-12 lg:grid-cols-[minmax(0,.9fr)_minmax(0,1.5fr)] lg:gap-16">
          <div className="min-w-0 space-y-9">
            {d ? (
              <>
                <div>
                  <h3 className="text-[15px] font-semibold text-faint">What it concluded</h3>
                  <p className="mt-3 text-pretty text-[clamp(1.35rem,2.1vw,1.8rem)] font-medium leading-[1.3] tracking-[-0.01em]">{firstSentence(d.root_cause, 420)}</p>
                </div>
                <div className="border-t border-line">
                  <details className="group border-b border-line">
                    <summary className="flex min-h-14 items-center justify-between text-[17px] font-semibold">
                      Read the full explanation
                      <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
                    </summary>
                    <div className="space-y-4 pb-5 text-[17px] leading-[1.6] text-muted">
                      <p><b className="font-semibold text-ink">Cause.</b> {d.root_cause}</p>
                      <p><b className="font-semibold text-ink">Mechanism.</b> {d.mechanism}</p>
                      <p><b className="font-semibold text-ink">Proposed repair.</b> {d.proposed_fix}</p>
                    </div>
                  </details>
                  {d.supporting_evidence.length ? (
                    <details className="group border-b border-line">
                      <summary className="flex min-h-14 items-center justify-between text-[17px] font-semibold">
                        Evidence it cited ({d.supporting_evidence.length})
                        <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
                      </summary>
                      <p className="mono pb-5 text-muted">{d.supporting_evidence.join(" · ")}</p>
                    </details>
                  ) : null}
                  <details className="group border-b border-line">
                    <summary className="flex min-h-14 items-center justify-between text-[17px] font-semibold">
                      Files it suspected ({d.suspected_files.length})
                      <span className="text-faint transition-transform group-open:rotate-45" aria-hidden>+</span>
                    </summary>
                    <ul className="pb-5">
                      {d.suspected_files.map((f) => (
                        <li key={f} className="py-1.5">
                          <span className="mono break-all">{f}</span>
                          <span className="block text-sm text-faint">{files.includes(f) ? "changed by the patch" : "suspected, left unchanged"}</span>
                        </li>
                      ))}
                    </ul>
                  </details>
                </div>
                {d.uncertainties.length ? (
                  <div className="border-2 border-dashed border-warn p-5">
                    <h3 className="text-[17px] font-bold text-warn">What the model says it cannot know</h3>
                    <p className="mt-2 text-[17px] leading-[1.55]">{d.uncertainties[0]}</p>
                    {d.uncertainties.length > 1 ? (
                      <details className="mt-3">
                        <summary className="min-h-11 py-2 text-[15px] font-semibold">{d.uncertainties.length - 1} more</summary>
                        <ul className="mt-2 list-disc space-y-2 pl-5 text-[16px] leading-[1.55] text-muted">
                          {d.uncertainties.slice(1).map((u) => <li key={u}>{u}</li>)}
                        </ul>
                      </details>
                    ) : null}
                  </div>
                ) : null}
              </>
            ) : (
              <p className="text-muted">This run exported no structured diagnosis.</p>
            )}

            <div data-testid="model-strip" className="border-t border-line pt-5 text-[15px] leading-[1.6] text-muted">
              <p><b className="font-semibold text-ink">{modelLabel(doc)}</b> · <span className="mono">{doc.run.model}</span></p>
              <p className="mt-1">
                {doc.run.model_calls.map((c) => `${c.purpose} ${seconds(c.latency_ms)}`).join(", ")} · {doc.run.prompt_tokens.toLocaleString("en-US")} in /{" "}
                {doc.run.completion_tokens.toLocaleString("en-US")} out tokens · recorded {utcTimestamp(doc.run.model_recorded_at ?? doc.run.finished_at)}
              </p>
              <p className="mt-1">{modelNote(doc)} Applied only to a disposable worktree, base {shortId(doc.run.base_revision, 10, 0)}.</p>
            </div>
          </div>

          <Reveal className="min-w-0">
            <div className="lg:sticky lg:top-20">
              <CodeViewer
                testId="demo-diff"
                heading="The patch, exactly as generated"
                file={files[0] ?? "patch"}
                diff={doc.patch.diff}
                digest={doc.patch.digest}
                notes={diffNotes(doc.patch.digest)}
                maxHeight="calc(100svh - 7rem)"
              />
              <p className="mt-3 text-[15px] leading-[1.55] text-faint">
                The numbered notes say what each line does. They do not claim which line fixed which invariant: the judge scored the patched candidate as a whole.
              </p>
            </div>
          </Reveal>
        </div>
      </div>
    </section>
  );
}
