"use client";

import { ArrowRight } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { diffNotes, failedIds, firstSentence } from "@/lib/evidence";
import { modelLabel, modelNote, shortId, utcTimestamp } from "@/lib/showcase";
import { CodeViewer } from "./code-viewer";
import { ActHeading, btnPrimary, Container, StatusMark } from "./ui";
import type { Candidate } from "./types";

export function FixAct({ doc, onChallenge }: { doc: DemoExport; onChallenge: (c: Candidate) => void }) {
  const d = doc.diagnosis;
  const files = doc.patch.changed_files;
  const seconds = (ms?: number) => (ms ? `${(ms / 1000).toFixed(1)}s` : "–");
  const calls = doc.run.model_calls;
  const flipped = failedIds(doc.pre_repair).filter((id) => doc.oracle.invariants.find((i) => i.id === id)?.status === "PASS");

  return (
    <section id="fix" className="scroll-mt-14 border-b border-line bg-surface">
      <Container className="py-20 sm:py-28">
        <ActHeading title={<>The AI rewrote {files.length === 1 ? "one file" : `${files.length} files`}.</>}>
          This is the model&rsquo;s recorded diagnosis and the exact patch it produced. Read both before you see how the
          judge scored it.
        </ActHeading>

        <p className="mt-8 max-w-[80ch] text-[15px] leading-[1.6] text-muted" data-testid="model-strip">
          <span className="font-medium text-ink">{modelLabel(doc)}:</span> <span className="mono">{doc.run.model}</span> ·{" "}
          {calls.map((c) => `${c.purpose} ${seconds(c.latency_ms)}`).join(", ")} · {doc.run.prompt_tokens.toLocaleString("en-US")} in /{" "}
          {doc.run.completion_tokens.toLocaleString("en-US")} out tokens · recorded {utcTimestamp(doc.run.model_recorded_at ?? doc.run.finished_at)}.{" "}
          {modelNote(doc)}
        </p>

        {d ? (
          <div className="mt-12 grid gap-10 lg:grid-cols-[1.3fr_1fr] lg:gap-16">
            <div>
              <h3 className="label">What it concluded</h3>
              <p className="mt-3 text-pretty text-[clamp(1.2rem,1.8vw,1.5rem)] font-medium leading-[1.4] tracking-[-0.01em]">
                {firstSentence(d.root_cause, 420)}
              </p>
              <details className="mt-4 border-y border-line">
                <summary className="flex min-h-12 items-center text-base font-medium">Read the full explanation</summary>
                <div className="space-y-4 pb-5 text-base leading-[1.65] text-muted">
                  <p><b className="font-semibold text-ink">Cause.</b> {d.root_cause}</p>
                  <p><b className="font-semibold text-ink">Mechanism.</b> {d.mechanism}</p>
                  <p><b className="font-semibold text-ink">Proposed repair.</b> {d.proposed_fix}</p>
                </div>
              </details>
            </div>
            <div className="space-y-8">
              {d.supporting_evidence.length ? (
                <div>
                  <h3 className="label">Evidence it cited</h3>
                  <p className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
                    {d.supporting_evidence.map((id) => (
                      <span key={id} className="mono text-base">{id}</span>
                    ))}
                  </p>
                  <p className="mt-1 text-sm text-faint">Ids from the evidence bundle the model was given.</p>
                </div>
              ) : null}
              <div>
                <h3 className="label">Files it suspected</h3>
                <ul className="mt-2 divide-y divide-line">
                  {d.suspected_files.map((f) => {
                    const patched = files.includes(f);
                    return (
                      <li key={f} className="py-2.5">
                        <span className="mono block break-all text-[15px]">{f}</span>
                        <span className={`text-[15px] ${patched ? "text-ok" : "text-muted"}`}>{patched ? "changed by the patch" : "suspected, left unchanged"}</span>
                      </li>
                    );
                  })}
                </ul>
              </div>
            </div>
          </div>
        ) : (
          <p className="mt-10 text-muted">This run exported no structured diagnosis.</p>
        )}

        {d?.uncertainties.length ? (
          <div className="mt-10 max-w-[860px] rounded-panel bg-warn-soft p-5">
            <h3 className="text-base font-semibold text-warn">What the model says it cannot know</h3>
            <p className="mt-2 text-base leading-[1.6] text-ink">{d.uncertainties[0]}</p>
            {d.uncertainties.length > 1 ? (
              <details className="mt-3">
                <summary className="min-h-10 py-1.5 text-[15px] font-medium text-ink">{d.uncertainties.length - 1} more</summary>
                <ul className="mt-2 list-disc space-y-2 pl-5 text-base leading-[1.6] text-muted">
                  {d.uncertainties.slice(1).map((u) => <li key={u}>{u}</li>)}
                </ul>
              </details>
            ) : null}
          </div>
        ) : null}

        <div className="mt-14">
          <CodeViewer
            testId="demo-diff"
            heading="The patch, exactly as generated"
            file={files[0] ?? "patch"}
            diff={doc.patch.diff}
            digest={doc.patch.digest}
            notes={diffNotes(doc.patch.digest)}
          />
          <p className="mt-3 max-w-[80ch] text-[15px] leading-[1.6] text-faint">
            {files.length} changed file{files.length === 1 ? "" : "s"} · base {shortId(doc.run.base_revision, 10, 0)} · applied only to a
            disposable worktree. The numbered notes say what each line does; they do not claim which line fixed which invariant.
          </p>
        </div>

        <div className="mt-16 grid gap-6 border-t border-line pt-10 lg:grid-cols-[1fr_auto] lg:items-end">
          <div className="max-w-[60ch]">
            <h3 className="text-2xl font-semibold tracking-[-0.02em]">The judge scored the patched candidate as a whole.</h3>
            <p className="mt-3 text-base leading-[1.6] text-muted">
              Before the patch, {failedIds(doc.pre_repair).join(", ") || "no invariants"} failed. After it, they pass. That
              clears the candidate, not any one line, so the next section puts a second patch to the same test.
            </p>
            <p className="mt-4 flex flex-wrap items-center gap-x-5 gap-y-2">
              {flipped.map((id) => (
                <span key={id} className="inline-flex items-center gap-2 text-base font-semibold">
                  {id} <StatusMark status="FAIL" compact /> <ArrowRight size={13} aria-hidden /> <StatusMark status="PASS" compact />
                </span>
              ))}
            </p>
          </div>
          <a href="#judge" onClick={() => onChallenge("unsafe")} className={btnPrimary}>
            Try to fool the judge
            <ArrowRight size={16} weight="bold" aria-hidden />
          </a>
        </div>
      </Container>
    </section>
  );
}
