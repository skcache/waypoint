"use client";

import Link from "next/link";
import { LockKey } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { humanGate } from "@/lib/evidence";
import { humanStatus, sourceLabel } from "@/lib/showcase";
import { Fact, YesNo } from "./kit";
import type { EvidenceSource } from "./types";

/** Step 4's stage: the human-review boundary, stated plainly. */
export function ReviewPanel({ doc, source, onEvidence }: { doc: DemoExport; source: EvidenceSource; onEvidence: () => void }) {
  const g = humanGate(doc);
  const h = doc.human_review;
  const applied = g.merged || g.deployed;
  return (
    <div data-testid="demo-human" className="flex min-h-0 flex-1 flex-col justify-between gap-10 overflow-auto rounded-[20px] bg-surface p-8 sm:p-12">
      <div>
        <p className="text-[15px] font-medium text-faint">Human review</p>
        <h2 className="mt-3 max-w-[18ch] text-[clamp(2rem,3.6vw,3.4rem)] font-medium leading-[1.05] tracking-[-0.03em]">
          {applied ? "The patch is applied" : "No person approved this patch"}
        </h2>
        <p className="mt-5 max-w-[56ch] text-[17px] leading-[1.6] text-muted">
          Waypoint stops at a patch for review. A person reads the diff and the checker result. The person then decides to merge or not. This page cannot merge, push,
          deploy, capture, refund, or retry a payment.
        </p>
        <div className="mt-14 flex max-w-[640px] items-end gap-4">
          <LockKey size={24} className="mb-2 shrink-0 text-faint" aria-hidden />
          <span className="shrink-0 text-[clamp(1.4rem,2.4vw,2rem)] font-medium">Approved by</span>
          <span className="mb-3 h-px flex-1 border-b-2 border-dotted border-faint" aria-hidden />
          <span className="mb-2 shrink-0 text-[15px] text-faint">no signature</span>
        </div>
      </div>

      <div className="grid gap-x-12 gap-y-8 lg:grid-cols-2">
        <dl>
          <Fact label="Review status">{humanStatus(h.status)}</Fact>
          <Fact label="Scope">{g.worktreeOnly ? "A disposable worktree only" : g.merged ? "The developer tree" : "No patch applied"}</Fact>
          <Fact label="Developer tree"><YesNo value={h.developer_tree_applied} /></Fact>
          <Fact label="Deployed"><YesNo value={h.deployed} /></Fact>
          <Fact label="Approval required"><YesNo value={h.human_approval_required} /></Fact>
          <Fact label="Evidence">{sourceLabel(doc, source)}, tier {doc.run.evidence_tier}</Fact>
        </dl>
        <div className="flex flex-col items-start justify-end gap-3">
          <p className="max-w-[44ch] text-[15px] leading-[1.55] text-muted">
            One case is a demonstration. The benchmark tests more incidents (preliminary result).
          </p>
          <div className="flex flex-wrap gap-3">
            <button type="button" onClick={onEvidence} className="inline-flex h-12 items-center rounded-[12px] bg-accent px-6 text-[16px] font-medium text-on-accent transition-[opacity,transform] hover:opacity-90 active:scale-[0.98]">
              Open evidence
            </button>
            <Link href="/benchmark" className="inline-flex h-12 items-center rounded-[12px] border border-line-strong px-6 text-[16px] font-medium transition-colors hover:bg-sunk">
              Benchmark
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
