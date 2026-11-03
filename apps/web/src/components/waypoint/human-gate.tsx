"use client";

import { useState } from "react";
import { CaretUp, LockKey } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { humanGate } from "@/lib/evidence";
import { humanStatus, sourceLabel } from "@/lib/showcase";
import { Fact, YesNo } from "./ui";
import type { EvidenceSource } from "./types";

/** Pinned on every scroll position: the candidate never leaves human review. */
export function HumanGate({ doc, source }: { doc: DemoExport; source: EvidenceSource }) {
  const [open, setOpen] = useState(false);
  const g = humanGate(doc);
  const h = doc.human_review;
  const chips = [
    g.worktreeOnly ? "Disposable worktree only" : g.merged ? "Applied to the developer tree" : "No patch applied",
    g.merged ? "Merged" : "Not merged",
    g.deployed ? "Deployed" : "Not deployed",
    g.approvalRequired ? "Human approval required" : "No approval required",
  ];
  return (
    <div className="night fixed inset-x-0 bottom-0 z-50 border-t border-line-strong bg-background" data-testid="human-gate">
      {open ? (
        <div id="gate-sheet" className="mx-auto max-h-[60dvh] w-full max-w-[1280px] overflow-auto border-b border-line px-5 py-6 sm:px-8 lg:px-12" data-testid="demo-human">
          <div className="grid gap-6 md:grid-cols-[1.1fr_.9fr]">
            <div>
              <h3 className="text-sm font-semibold text-warn">Human review boundary</h3>
              <p className="mt-2 text-2xl font-semibold leading-tight tracking-[-0.02em]">
                The candidate is ready for review. No approval has been recorded.
              </p>
              <p className="mt-3 max-w-[60ch] text-base leading-[1.6] text-muted">
                Waypoint stops at a reviewable candidate. A human decides whether the change is integrated. This page
                cannot merge, push, deploy, capture, refund or retry a payment.
              </p>
            </div>
            <dl className="divide-y divide-line">
              <Fact label="Review status">{humanStatus(h.status)}</Fact>
              <Fact label="Applied in worktree"><YesNo value={h.candidate_patch_applied_in_disposable_worktree} /></Fact>
              <Fact label="Developer tree applied"><YesNo value={h.developer_tree_applied} /></Fact>
              <Fact label="Deployed"><YesNo value={h.deployed} /></Fact>
              <Fact label="Approval required"><YesNo value={h.human_approval_required} /></Fact>
            </dl>
          </div>
        </div>
      ) : null}
      <div className="mx-auto flex min-h-[52px] w-full max-w-[1280px] items-center gap-3 px-4 sm:px-8 lg:px-12">
        <span className="inline-flex shrink-0 items-center gap-2 text-sm font-semibold text-warn">
          <LockKey size={18} weight="fill" aria-hidden />
          <span className="hidden sm:inline">Human gate</span>
        </span>
        <ul className="scrollbar-none flex min-w-0 flex-1 items-center gap-x-3 overflow-x-auto text-[15px] whitespace-nowrap text-muted" aria-label="Review boundary">
          <li className="md:hidden">{g.merged || g.deployed ? "Candidate applied: see details" : "Not merged or deployed"}</li>
          {chips.map((c, i) => (
            <li key={c} className="hidden md:block">
              {i > 0 ? <span className="mr-3 text-line-strong" aria-hidden>·</span> : null}
              {c}
            </li>
          ))}
          <li className="hidden text-faint lg:block"><span className="mr-3 text-line-strong" aria-hidden>·</span>{sourceLabel(doc, source)}, tier {doc.run.evidence_tier}</li>
        </ul>
        <button
          type="button"
          aria-expanded={open}
          aria-controls="gate-sheet"
          onClick={() => setOpen((v) => !v)}
          className="inline-flex h-11 shrink-0 items-center gap-1.5 rounded border border-line-strong px-4 text-[15px] font-medium text-ink transition-colors hover:border-ink"
        >
          Details
          <CaretUp size={14} weight="bold" className={`transition-transform ${open ? "rotate-180" : ""}`} aria-hidden />
        </button>
      </div>
    </div>
  );
}
