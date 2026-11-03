"use client";

import { useState } from "react";
import { LockKey } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { humanGate } from "@/lib/evidence";
import { humanStatus, sourceLabel } from "@/lib/showcase";
import { Fact, YesNo } from "./kit";
import type { EvidenceSource } from "./types";

/**
 * The human-review boundary, pinned to the bottom of every screen as an empty
 * signature line. Nothing here has been approved, and this page cannot approve.
 */
export function SignatureBar({ doc, source }: { doc: DemoExport; source: EvidenceSource }) {
  const [open, setOpen] = useState(false);
  const g = humanGate(doc);
  const h = doc.human_review;
  const applied = g.merged || g.deployed;
  return (
    <div data-testid="human-gate" className="field-ink fixed inset-x-0 bottom-0 z-50 border-t-2 border-paper">
      {open ? (
        <div id="gate-sheet" data-testid="demo-human" className="mx-auto max-h-[62dvh] w-full max-w-[1440px] overflow-auto px-5 py-6 sm:px-8 lg:px-12">
          <div className="grid gap-8 md:grid-cols-2">
            <div>
              <h3 className="poster text-[34px]">No approval has been recorded.</h3>
              <p className="mt-4 max-w-[50ch] text-[17px] leading-[1.55] text-muted">
                Waypoint stops at a reviewable candidate. A human decides whether it is integrated. This page cannot merge, push, deploy, capture, refund or retry a
                payment.
              </p>
            </div>
            <dl>
              <Fact label="Review status">{humanStatus(h.status)}</Fact>
              <Fact label="Scope">{g.worktreeOnly ? "Disposable worktree only" : g.merged ? "Applied to the developer tree" : "No patch applied"}</Fact>
              <Fact label="Developer tree"><YesNo value={h.developer_tree_applied} /></Fact>
              <Fact label="Deployed"><YesNo value={h.deployed} /></Fact>
              <Fact label="Approval required"><YesNo value={h.human_approval_required} /></Fact>
              <Fact label="Evidence">{sourceLabel(doc, source)}, tier {doc.run.evidence_tier}</Fact>
            </dl>
          </div>
        </div>
      ) : null}
      <button
        type="button"
        aria-expanded={open}
        aria-controls="gate-sheet"
        onClick={() => setOpen((v) => !v)}
        className="mx-auto flex min-h-14 w-full max-w-[1440px] items-center gap-4 px-5 text-left sm:px-8 lg:px-12"
      >
        <LockKey size={20} weight="fill" className="shrink-0 text-clear" aria-hidden />
        <span className="poster shrink-0 text-[20px]">{applied ? "Candidate applied" : "Candidate only"}</span>
        <span className="hidden shrink-0 text-[15px] text-muted sm:inline">Approved by</span>
        <span className="mb-[-2px] hidden h-px flex-1 self-end border-b-2 border-dotted border-muted max-sm:hidden sm:block" aria-hidden />
        <span className="min-w-0 flex-1 truncate text-[15px] text-muted sm:flex-none">{applied ? "see details" : <><span className="sm:hidden">unsigned</span><span className="max-sm:hidden">awaiting a human signature</span></>}</span>
        <span className="shrink-0 text-[15px] font-semibold">{open ? "Close" : "Details"}</span>
      </button>
    </div>
  );
}
