import type { Metadata } from "next";
import { Panel } from "@/components/panel";
import { SampleLabel } from "@/components/sample-label";

export const metadata: Metadata = {
  title: "Workbench",
};

const systems = [
  {
    name: "API runtime",
    state: "In place",
    tone: "text-ok",
    note: "Go API with liveness and database-readiness endpoints. This UI does not call it yet.",
  },
  {
    name: "Database",
    state: "In place",
    tone: "text-ok",
    note: "PostgreSQL schema with versioned migrations. No UI reads it yet.",
  },
  {
    name: "Runtime AI",
    state: "Not configured",
    tone: "text-muted",
    note: "Intent drafting and semantic verification arrive in later phases.",
  },
  {
    name: "PayPal checkout",
    state: "Not wired",
    tone: "text-muted",
    note: "The sandbox flow was proven server-side in the Phase 04 spike. Product checkout wiring is not built yet.",
  },
];

const mandate: [string, string][] = [
  ["Currency", "USD"],
  ["Ceiling", "$150.00"],
  ["Max quantity", "1"],
  ["Category", "keyboard"],
  ["Preferences", "quiet, office use, no gaming RGB"],
  ["Expires", "2026-10-24 18:00 UTC"],
];

const proposal: [string, string][] = [
  ["Catalog item", "keyboard-office-quiet"],
  ["Description", "Quiet office keyboard, 87-key, non-backlit"],
  ["Unit price", "$142.00"],
  ["Quantity", "1"],
  ["Total", "$142.00"],
  ["Agent rationale", "Selected for quiet office typing"],
];

const decisions = [
  {
    state: "Allow",
    tone: "text-ok",
    rule: "Hard checks pass and the semantic verifier supports the fit.",
    effect: "Eligible for one PayPal sandbox checkout.",
  },
  {
    state: "Deny",
    tone: "text-bad",
    rule: "A hard-rule violation, or a qualitative contradiction with cited evidence.",
    effect: "Never calls the payment provider.",
  },
  {
    state: "Review",
    tone: "text-warn",
    rule: "Missing evidence, model UNKNOWN, timeout, or ambiguity.",
    effect: "Fails closed. Not payable until a human resolves it.",
  },
];

const scenarios = [
  {
    tag: "Scenario A - authorized checkout",
    rows: [
      ["Human confirmed", "One quiet office keyboard, up to $150.00, quantity 1."],
      [
        "Agent proposed",
        "Quiet office keyboard, 87-key, non-backlit at $142.00 from the controlled catalog.",
      ],
      [
        "Waypoint decided",
        "Allow. Hard checks pass and the verifier supports the fit.",
      ],
      [
        "PayPal",
        "Not executed in this preview. In a wired run, the buyer approves on PayPal, the server captures, and the result appears only when the provider confirms it.",
      ],
    ],
  },
  {
    tag: "Scenario B - qualitative mismatch",
    rows: [
      [
        "Human confirmed",
        "Quiet keyboard for office work. No loud gaming keyboards, no RGB.",
      ],
      [
        "Agent proposed",
        "Clicky gaming keyboard with bright RGB lighting at $120.00. Amount and quantity pass the numeric checks.",
      ],
      [
        "Waypoint decided",
        "Deny, or review. The verifier finds the contradiction between the evidence and the confirmed preferences.",
      ],
      ["PayPal", "No order is created. A denied or reviewed proposal never reaches the provider."],
    ],
  },
  {
    tag: "Scenario C - ambiguous provider outcome (fault injection)",
    rows: [
      [
        "Context",
        "A mocked capture times out with an unknown outcome while a duplicate client request arrives. This is controlled fault injection, not a live provider outage.",
      ],
      [
        "Waypoint decided",
        "One logical operation is preserved with the recorded provider request ID. The run moves to reconciliation instead of retrying blindly.",
      ],
      [
        "PayPal",
        "Nothing is recorded as failed or completed without evidence. A status check or an idempotent retry resolves the outcome.",
      ],
    ],
  },
];

export default function WorkbenchPage() {
  return (
    <div className="space-y-10">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">Workbench</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
          The intended flow, end to end: what the human confirmed, what the
          agent proposed, what Waypoint decided, and what PayPal confirmed.
          Nothing on this page runs; every value is a labeled sample.
        </p>
      </header>

      <section
        aria-labelledby="flow-heading"
        className="grid gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]"
      >
        <h2 id="flow-heading" className="sr-only">
          Confirmed mandate and agent proposal
        </h2>
        <Panel title="Request and confirmed mandate" label={<SampleLabel />}>
          <p className="text-sm leading-6">
            &ldquo;Buy one quiet office keyboard, under $150.&rdquo;
          </p>
          <p className="mt-1 text-xs text-muted">
            Human confirmation happens in a later phase. This draft is not
            spendable.
          </p>
          <dl className="mt-4 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[minmax(0,9rem)_minmax(0,1fr)]">
            {mandate.map(([key, value]) => (
              <div key={key} className="contents">
                <dt className="text-muted">{key}</dt>
                <dd className="font-mono text-[13px]">{value}</dd>
              </div>
            ))}
          </dl>
        </Panel>

        <Panel title="Agent proposal" label={<SampleLabel />}>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[minmax(0,8rem)_minmax(0,1fr)]">
            {proposal.map(([key, value]) => (
              <div key={key} className="contents">
                <dt className="text-muted">{key}</dt>
                <dd className="font-mono text-[13px]">{value}</dd>
              </div>
            ))}
          </dl>
          <p className="mt-4 border-t border-line pt-3 text-xs leading-5 text-muted">
            The agent proposes. It never grants permission, and its rationale is
            evidence, not authorization.
          </p>
        </Panel>
      </section>

      <section aria-labelledby="decisions-heading">
        <h2
          id="decisions-heading"
          className="text-xs font-medium uppercase tracking-wider text-muted"
        >
          Decision outcomes
        </h2>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
          Every proposal resolves to exactly one of these. The runtime that
          produces them live arrives in later phases.
        </p>
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          {decisions.map((decision) => (
            <div
              key={decision.state}
              className="rounded-panel border border-line bg-surface px-4 py-4"
            >
              <p className={`text-sm font-medium ${decision.tone}`}>
                {decision.state}
              </p>
              <p className="mt-2 text-xs leading-5 text-muted">{decision.rule}</p>
              <p className="mt-3 border-t border-line pt-3 text-xs leading-5">
                {decision.effect}
              </p>
            </div>
          ))}
        </div>
      </section>

      <section aria-labelledby="scenarios-heading">
        <h2
          id="scenarios-heading"
          className="text-xs font-medium uppercase tracking-wider text-muted"
        >
          Demonstration scenarios
        </h2>
        <div className="mt-4 space-y-6">
          {scenarios.map((scenario) => (
            <Panel
              key={scenario.tag}
              title={scenario.tag}
              label={<SampleLabel>Illustrative</SampleLabel>}
            >
              <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[minmax(0,9rem)_minmax(0,1fr)]">
                {scenario.rows.map(([key, value]) => (
                  <div key={key} className="contents">
                    <dt className="text-muted">{key}</dt>
                    <dd className="leading-6">{value}</dd>
                  </div>
                ))}
              </dl>
            </Panel>
          ))}
        </div>
      </section>

      <Panel title="System status" label={<SampleLabel>As of Phase 05</SampleLabel>}>
        <ul className="divide-y divide-line">
          {systems.map((system) => (
            <li
              key={system.name}
              className="grid gap-1 py-3 first:pt-0 last:pb-0 sm:grid-cols-[minmax(0,10rem)_minmax(0,7rem)_minmax(0,1fr)] sm:gap-6"
            >
              <p className="text-sm font-medium">{system.name}</p>
              <p className={`text-sm ${system.tone}`}>{system.state}</p>
              <p className="text-sm leading-6 text-muted">{system.note}</p>
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}
