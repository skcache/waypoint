import type { Metadata } from "next";
import { Panel } from "@/components/panel";
import { SampleLabel } from "@/components/sample-label";

export const metadata: Metadata = {
  title: "Checkout",
};

const summary: [string, string][] = [
  ["Item", "Quiet office keyboard, 87-key, non-backlit"],
  ["Quantity", "1"],
  ["Unit price", "$142.00"],
  ["Total", "$142.00"],
  ["Currency", "USD"],
  ["Provider", "PayPal sandbox"],
];

const lifecycle = [
  {
    state: "Awaiting buyer approval",
    tone: "text-muted",
    note: "The order exists; the buyer has not approved yet.",
  },
  {
    state: "Buyer approved",
    tone: "text-muted",
    note: "Approval is recorded. Approval is not payment completion.",
  },
  {
    state: "Capture pending",
    tone: "text-warn",
    note: "The server asked the provider to capture. The outcome is not known yet.",
  },
  {
    state: "Reconciliation required",
    tone: "text-warn",
    note: "The provider outcome is unclear. The operation keeps its identity and resolves only on sufficient provider evidence; no blind retries.",
  },
  {
    state: "Completed",
    tone: "text-ok",
    note: "Shown only when the provider confirms the capture, with matching amount and currency.",
  },
  {
    state: "Cancelled",
    tone: "text-muted",
    note: "The buyer cancelled before capture. No charge.",
  },
  {
    state: "Failed",
    tone: "text-bad",
    note: "Recorded only with evidence of terminal failure, never from a timeout.",
  },
];

const previewStates = [
  {
    name: "Not configured",
    note: "PayPal credentials absent. The page says so plainly and stays inert.",
  },
  {
    name: "Awaiting buyer approval",
    note: "The payment area waits on PayPal; nothing here fakes an approval.",
  },
  {
    name: "Reconciliation required",
    note: "The status explains the uncertainty and what happens next.",
  },
  {
    name: "Completed (provider-confirmed)",
    note: "The only state that may claim payment, backed by the provider capture.",
  },
  {
    name: "Explicit error",
    note: "Errors are stated inline with a reason, never as a generic failure toast.",
  },
];

export default function CheckoutPage() {
  return (
    <div className="space-y-10">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">Checkout</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
          How a transaction checkout is meant to look: an immutable purchase
          summary on the left, the PayPal payment step on the right. This
          preview is static and wired to nothing.
        </p>
      </header>

      <section
        aria-labelledby="checkout-grid-heading"
        className="grid gap-6 lg:grid-cols-[minmax(0,6fr)_minmax(0,6fr)]"
      >
        <h2 id="checkout-grid-heading" className="sr-only">
          Purchase summary and payment
        </h2>
        <Panel title="Purchase summary" label={<SampleLabel />}>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[minmax(0,8rem)_minmax(0,1fr)]">
            {summary.map(([key, value]) => (
              <div key={key} className="contents">
                <dt className="text-muted">{key}</dt>
                <dd className="font-mono text-[13px]">{value}</dd>
              </div>
            ))}
          </dl>
          <p className="mt-4 border-t border-line pt-3 text-xs leading-5 text-muted">
            Once authorized, this summary is immutable. The server reconstructs
            amount and item from stored records, never from browser values.
          </p>
        </Panel>

        <Panel
          title="Payment"
          label={<SampleLabel>Not connected</SampleLabel>}
        >
          <div className="rounded-panel border border-dashed border-line-strong px-4 py-8 text-center">
            <p className="text-sm font-medium">PayPal payment step</p>
            <p className="mx-auto mt-2 max-w-sm text-xs leading-5 text-muted">
              The PayPal SDK v6 payment UI mounts here once checkout wiring
              lands. Buyer approval happens on PayPal, and the server captures
              only after it verifies that approval.
            </p>
          </div>
          <p className="mt-4 text-xs leading-5 text-muted">
            Sandbox only. Buyer approval is not payment completion: the
            provider-confirmed capture decides, and until then this page claims
            nothing.
          </p>
        </Panel>
      </section>

      <section aria-labelledby="lifecycle-heading">
        <h2
          id="lifecycle-heading"
          className="text-xs font-medium uppercase tracking-wider text-muted"
        >
          Status wording
        </h2>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">
          The vocabulary a wired checkout uses, chosen so that AI assessment,
          human approval, and provider confirmation never blur together.
        </p>
        <ul className="mt-4 divide-y divide-line border-y border-line">
          {lifecycle.map((item) => (
            <li
              key={item.state}
              className="grid gap-1 py-3 sm:grid-cols-[minmax(0,13rem)_minmax(0,1fr)] sm:gap-6"
            >
              <p className={`text-sm font-medium ${item.tone}`}>{item.state}</p>
              <p className="text-sm leading-6 text-muted">{item.note}</p>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="preview-states-heading">
        <h2
          id="preview-states-heading"
          className="text-xs font-medium uppercase tracking-wider text-muted"
        >
          Preview states
        </h2>
        <div className="mt-4 grid gap-4 md:grid-cols-2">
          {previewStates.map((state) => (
            <div
              key={state.name}
              className="rounded-panel border border-line bg-surface px-4 py-4"
            >
              <p className="text-sm font-medium">{state.name}</p>
              <p className="mt-2 text-xs leading-5 text-muted">{state.note}</p>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
