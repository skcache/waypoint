"use client";

import { useState } from "react";

type Decision = "accept_for_integration" | "request_changes" | "reject";

const options: { value: Decision; label: string; tone: string }[] = [
  { value: "accept_for_integration", label: "Accept for integration", tone: "border-ok/40 text-ok" },
  { value: "request_changes", label: "Request changes", tone: "border-warn/40 text-warn" },
  { value: "reject", label: "Reject", tone: "border-bad/40 text-bad" },
];

export function DecisionForm({ caseId, condition }: { caseId: string; condition: string }) {
  const [choice, setChoice] = useState<Decision | null>(null);
  const [note, setNote] = useState("");
  const [state, setState] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [message, setMessage] = useState("");

  async function submit() {
    if (!choice) return;
    setState("saving");
    try {
      const res = await fetch("/api/decisions", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ case_id: caseId, condition, decision: choice, note }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body?.error ?? "could not record decision");
      setState("saved");
      setMessage(body.recorded_at ? `Recorded ${new Date(body.recorded_at).toLocaleString()}` : "Recorded");
    } catch (err) {
      setState("error");
      setMessage(err instanceof Error ? err.message : "could not record decision");
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => setChoice(option.value)}
            className={`rounded-md border bg-surface px-2.5 py-1.5 text-[11px] font-medium transition-colors ${
              choice === option.value
                ? option.tone
                : "border-line text-muted hover:border-line-strong hover:text-ink"
            }`}
          >
            {option.label}
          </button>
        ))}
      </div>
      <textarea
        value={note}
        onChange={(event) => setNote(event.target.value)}
        rows={3}
        placeholder="Review note (optional)"
        className="w-full resize-none rounded-md border border-line bg-background px-2.5 py-2 text-xs leading-5 text-ink outline-none placeholder:text-faint focus:border-accent"
      />
      <div className="flex items-center justify-between gap-3">
        <button
          type="button"
          disabled={!choice || state === "saving"}
          onClick={submit}
          className="rounded-md bg-ink px-3 py-1.5 text-[11px] font-medium text-background disabled:opacity-40"
        >
          {state === "saving" ? "Recording…" : "Record decision"}
        </button>
        <span className={`text-[11px] ${state === "error" ? "text-bad" : "text-faint"}`}>
          {state === "saved" ? message : state === "error" ? message : "Local record only. No merge, deploy or payment action."}
        </span>
      </div>
    </div>
  );
}
