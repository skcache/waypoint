"use client";

import { useEffect, useState } from "react";

type Decision = "accept_for_integration" | "request_changes" | "reject";

const options: { value: Decision; label: string; tone: string }[] = [
  { value: "accept_for_integration", label: "Accept", tone: "border-ok/40 text-ok" },
  { value: "request_changes", label: "Request changes", tone: "border-warn/40 text-warn" },
  { value: "reject", label: "Reject", tone: "border-bad/40 text-bad" },
];

export function DecisionForm({ caseId, condition }: { caseId: string; condition: string }) {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [token, setToken] = useState("");
  const [choice, setChoice] = useState<Decision | null>(null);
  const [note, setNote] = useState("");
  const [state, setState] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [message, setMessage] = useState("");

  useEffect(() => {
    let live = true;
    fetch("/api/decisions")
      .then((res) => res.json())
      .then((body: { enabled?: boolean }) => {
        if (live) setEnabled(Boolean(body.enabled));
      })
      .catch(() => {
        if (live) setEnabled(false);
      });
    return () => {
      live = false;
    };
  }, []);

  async function submit() {
    if (!choice || !token) return;
    setState("saving");
    try {
      const res = await fetch("/api/decisions", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ case_id: caseId, condition, decision: choice, note }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body?.error ?? "could not record decision");
      setState("saved");
      setMessage(body.recorded_at ? `Recorded ${new Date(body.recorded_at).toLocaleString()}` : "Recorded");
      setToken("");
    } catch (err) {
      setState("error");
      setMessage(err instanceof Error ? err.message : "could not record decision");
    }
  }

  if (enabled === null) {
    return <p className="text-[13px] text-faint">Checking the review setting…</p>;
  }

  if (!enabled) {
    return (
      <div className="space-y-2">
        <p className="text-[13px] leading-5 text-muted">
          Review recording is <span className="text-ink">off</span> in this
          deployment. The console is read-only. An anonymous visitor cannot record an approval.
        </p>
        <p className="text-[13px] leading-4 text-faint">
          An operator turns it on with a reviewer token in the server environment.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <input
        type="password"
        value={token}
        onChange={(event) => setToken(event.target.value)}
        placeholder="Reviewer token"
        autoComplete="off"
        className="w-full rounded-[8px] border border-line bg-background px-3 py-2 text-[13px] text-ink outline-none placeholder:text-faint focus:border-accent"
      />
      <div className="flex flex-wrap gap-2">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => setChoice(option.value)}
            className={`rounded-[8px] border bg-surface px-3 py-2 text-[13px] font-medium transition-colors ${
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
        placeholder="Note (optional)"
        className="w-full resize-none rounded-[8px] border border-line bg-background px-3 py-2 text-[13px] leading-5 text-ink outline-none placeholder:text-faint focus:border-accent"
      />
      <div className="flex items-center justify-between gap-3">
        <button
          type="button"
          disabled={!choice || !token || state === "saving"}
          onClick={submit}
          className="rounded-[8px] bg-accent px-4 py-2 text-[13px] font-medium text-on-accent transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          {state === "saving" ? "Recording" : "Record decision"}
        </button>
        <span className={`text-[13px] ${state === "error" ? "text-bad" : "text-faint"}`}>
          {state === "saved" || state === "error"
            ? message
            : "Local note only. No merge, deploy, or payment action."}
        </span>
      </div>
    </div>
  );
}
