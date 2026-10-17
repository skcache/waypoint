import type { Metadata } from "next";
import { Panel } from "@/components/panel";
import { SampleLabel } from "@/components/sample-label";

export const metadata: Metadata = {
  title: "Trace",
};

const sourceTone: Record<string, string> = {
  MODEL: "text-accent",
  POLICY: "text-ink",
  RUNTIME: "text-muted",
  PAYPAL: "text-ok",
};

const events = [
  {
    time: "10:41:02.118",
    source: "RUNTIME",
    title: "Transaction created",
    meta: "state PROPOSED",
    duration: "0 ms",
    tone: "text-muted",
  },
  {
    time: "10:41:02.641",
    source: "MODEL",
    title: "Semantic verification: SUPPORTED",
    meta: "classifier sample-model, cited evidence",
    duration: "1.9 s",
    tone: "text-ok",
  },
  {
    time: "10:41:04.203",
    source: "POLICY",
    title: "Decision: ALLOW",
    meta: "reason HARD_CHECKS_PASS",
    duration: "4 ms",
    tone: "text-ok",
  },
  {
    time: "10:41:04.511",
    source: "PAYPAL",
    title: "Sandbox order created",
    meta: "order SAMPLE-ORDER-001",
    duration: "612 ms",
    tone: "text-muted",
  },
  {
    time: "10:47:31.090",
    source: "PAYPAL",
    title: "Buyer approved",
    meta: "approval is not completion",
    duration: "-",
    tone: "text-muted",
  },
  {
    time: "10:47:33.844",
    source: "PAYPAL",
    title: "Capture confirmed: COMPLETED",
    meta: "capture SAMPLE-CAPTURE-001, USD 142.00",
    duration: "940 ms",
    tone: "text-ok",
  },
];

const samplePayload = `{
  "seq": 6,
  "event_type": "PROVIDER_CAPTURE_CONFIRMED",
  "source": "PAYPAL",
  "details": {
    "order_id": "SAMPLE-ORDER-001",
    "capture_id": "SAMPLE-CAPTURE-001",
    "status": "COMPLETED",
    "amount": { "currency": "USD", "value": "142.00" },
    "payload_fingerprint": "sha256:2f6c...sample"
  }
}`;

export default function TracePage() {
  return (
    <div className="space-y-10">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">Trace</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
          Why a purchase was allowed, blocked, or left unresolved: a
          chronological timeline across MODEL, POLICY, RUNTIME and PAYPAL
          steps, with sanitized event details.
        </p>
      </header>

      <Panel title="Events" label={<SampleLabel>Sample trace</SampleLabel>}>
        <p className="mb-4 rounded-panel border border-dashed border-line-strong px-4 py-3 text-xs leading-5 text-muted">
          No events yet. The timeline below is a sample of the intended shape;
          real events appear here once the runtime records transactions in
          later phases. Nothing below comes from a live transaction.
        </p>
        <ol className="divide-y divide-line">
          {events.map((event) => (
            <li
              key={`${event.time}-${event.title}`}
              className="grid gap-x-6 gap-y-1 py-3 sm:grid-cols-[minmax(0,7rem)_minmax(0,5rem)_minmax(0,1fr)_minmax(0,4rem)]"
            >
              <p className="font-mono text-xs text-muted">{event.time}</p>
              <p
                className={`font-mono text-[11px] tracking-wide ${sourceTone[event.source]}`}
              >
                {event.source}
              </p>
              <div>
                <p className={`text-sm ${event.tone}`}>{event.title}</p>
                <p className="mt-0.5 font-mono text-xs text-faint">
                  {event.meta}
                </p>
              </div>
              <p className="font-mono text-xs text-muted sm:text-right">
                {event.duration}
              </p>
            </li>
          ))}
        </ol>
      </Panel>

      <Panel title="Sample event payload" label={<SampleLabel />}>
        <details>
          <summary className="text-sm text-accent">
            Show one event payload (sanitized)
          </summary>
          <pre className="mt-3 overflow-x-auto rounded-panel border border-line bg-background p-4 font-mono text-xs leading-5">
            {samplePayload}
          </pre>
        </details>
        <p className="mt-4 text-xs leading-5 text-muted">
          Event payloads are sanitized server-side. Credentials, authorization
          headers, and raw provider bodies never appear in the trace. Filters
          by source and state arrive with the runtime.
        </p>
      </Panel>

      <Panel title="Failure states" label={<SampleLabel>Illustrative</SampleLabel>}>
        <ul className="divide-y divide-line">
          <li className="grid gap-1 py-3 first:pt-0 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)] sm:gap-6">
            <p className="text-sm font-medium text-warn">Reconciliation required</p>
            <p className="text-sm leading-6 text-muted">
              The provider outcome is unclear. The timeline shows the recorded
              request ID and the follow-up checks instead of a guessed result,
              and the state resolves only when sufficient evidence arrives.
            </p>
          </li>
          <li className="grid gap-1 py-3 last:pb-0 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)] sm:gap-6">
            <p className="text-sm font-medium text-bad">Denied</p>
            <p className="text-sm leading-6 text-muted">
              A policy or verifier denial, with the reason code and the cited
              evidence. No provider call appears because none was made.
            </p>
          </li>
        </ul>
      </Panel>
    </div>
  );
}
