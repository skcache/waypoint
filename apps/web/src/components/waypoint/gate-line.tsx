"use client";

import { useState } from "react";

import type { DemoExport } from "@/lib/demo";
import { humanGate } from "@/lib/evidence";
import { humanStatus, sourceLabel } from "@/lib/showcase";
import { Fact, LockKey, YesNo } from "./kit";
import type { EvidenceSource } from "./types";

/**
 * The human-review boundary. It sits at the foot of the rail on desktop and as
 * a fixed bar on phones, so it is on screen at every scroll position.
 */
export function GateLine({ doc, source }: { doc: DemoExport; source: EvidenceSource }) {
  const [open, setOpen] = useState(false);
  const g = humanGate(doc);
  const h = doc.human_review;
  const applied = g.merged || g.deployed;
  const summary = applied ? "applied, see details" : g.approvalRequired ? "approval needed" : "no approval needed";
  return (
    <div
      data-testid="human-gate"
      className="relative shrink-0 border-t border-line bg-background max-lg:fixed max-lg:inset-x-0 max-lg:bottom-0 max-lg:z-50 max-lg:px-4 lg:mt-4"
    >
      {open ? (
        <div id="gate-sheet" className="max-h-[60dvh] overflow-auto py-4 lg:absolute lg:inset-x-0 lg:bottom-full lg:z-20 lg:mb-3 lg:rounded-2xl lg:border lg:border-line-strong lg:bg-cell-dark lg:p-5" data-testid="demo-human">
          <h3 className="text-[15px] font-semibold">The candidate is ready for review. No approval has been recorded.</h3>
          <p className="mt-2 text-[15px] leading-[1.6] text-muted">
            Waypoint stops at a reviewable candidate. A human decides whether it is integrated. This page cannot merge, push, deploy,
            capture, refund or retry a payment.
          </p>
          <dl className="mt-3">
            <Fact label="Review status">{humanStatus(h.status)}</Fact>
            <Fact label="Scope">{g.worktreeOnly ? "Disposable worktree only" : g.merged ? "Applied to the developer tree" : "No patch applied"}</Fact>
            <Fact label="Applied in worktree"><YesNo value={h.candidate_patch_applied_in_disposable_worktree} /></Fact>
            <Fact label="Developer tree"><YesNo value={h.developer_tree_applied} /></Fact>
            <Fact label="Deployed"><YesNo value={h.deployed} /></Fact>
            <Fact label="Approval required"><YesNo value={h.human_approval_required} /></Fact>
            <Fact label="Evidence">{sourceLabel(doc, source)}, tier {doc.run.evidence_tier}</Fact>
          </dl>
        </div>
      ) : null}
      <button
        type="button"
        aria-expanded={open}
        aria-controls="gate-sheet"
        onClick={() => setOpen((v) => !v)}
        className="flex min-h-12 w-full items-center gap-3 py-2 text-left"
      >
        <LockKey size={18} weight="fill" className="shrink-0 text-warn" aria-hidden />
        <span className="min-w-0 flex-1 truncate text-[15px] text-muted">
          <span className="font-medium text-ink">{applied ? "Candidate applied" : "Candidate only"}</span> · {summary}
        </span>
        <span className="text-[15px] text-faint">{open ? "Close" : "Details"}</span>
      </button>
    </div>
  );
}
