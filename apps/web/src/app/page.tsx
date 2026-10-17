const status: { area: string; state: string; note: string }[] = [
  {
    area: "API runtime — Go",
    state: "Scaffolded",
    note: "Liveness endpoint GET /healthz implemented with automated tests.",
  },
  {
    area: "Web interface — Next.js",
    state: "Scaffolded",
    note: "This page only. No interactive flows exist yet.",
  },
  {
    area: "PostgreSQL",
    state: "Not connected",
    note: "Local service defined in compose.yaml. Schema and migrations arrive in a later phase.",
  },
  {
    area: "PayPal sandbox — Orders v2",
    state: "Not configured",
    note: "Checkout, buyer approval and capture are planned. No provider calls exist yet.",
  },
  {
    area: "Runtime AI — intent compiler, semantic verifier",
    state: "Not configured",
    note: "Model-backed verification is planned. No AI is running here yet.",
  },
];

const planned = [
  "Human-confirmed purchase mandates with enforced limits — currency, ceiling, quantity, expiration.",
  "Deterministic policy plus semantic verification: hard rules always win; ambiguous cases fail closed to review.",
  "PayPal sandbox execution — one-time order, buyer approval, server-side capture, no blind retries on unknown outcomes.",
  "Correlated transaction traces that explain why a purchase was allowed, blocked, or left unresolved.",
];

export default function Home() {
  return (
    <div className="mx-auto flex min-h-dvh max-w-3xl flex-col px-5 py-10 sm:px-8 sm:py-14">
      <header className="flex items-baseline justify-between gap-4">
        <span className="text-sm font-semibold tracking-tight">Waypoint</span>
        <span className="text-[11px] uppercase tracking-wider text-zinc-500">
          PayPal AI Hackathon 2026
        </span>
      </header>

      <main className="mt-14 flex-1 sm:mt-20">
        <h1 className="max-w-2xl text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
          An intent-aware execution boundary for AI-initiated financial
          transactions.
        </h1>
        <p className="mt-5 max-w-2xl text-sm leading-6 text-zinc-600 sm:text-base sm:leading-7">
          Waypoint checks an AI agent&apos;s proposed purchase against a
          human-confirmed mandate and deterministic policy before anything
          reaches a payment provider — then records a correlated trace of every
          decision and outcome.
        </p>
        <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-500">
          This site is the Phase 01 scaffold. No integrations are configured
          yet and no payments are processed.
        </p>

        <section className="mt-14" aria-labelledby="status-heading">
          <h2
            id="status-heading"
            className="text-xs font-medium uppercase tracking-wider text-zinc-500"
          >
            Development status
          </h2>
          <ul className="mt-4 divide-y divide-zinc-200 border-y border-zinc-200">
            {status.map((item) => (
              <li
                key={item.area}
                className="grid gap-1 py-4 sm:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] sm:gap-6"
              >
                <div>
                  <p className="text-sm font-medium">{item.area}</p>
                  <p className="mt-0.5 text-xs text-zinc-500">{item.state}</p>
                </div>
                <p className="text-sm leading-6 text-zinc-600">{item.note}</p>
              </li>
            ))}
          </ul>
        </section>

        <section className="mt-14" aria-labelledby="planned-heading">
          <h2
            id="planned-heading"
            className="text-xs font-medium uppercase tracking-wider text-zinc-500"
          >
            Planned capabilities
          </h2>
          <p className="mt-3 text-xs text-zinc-500">
            Locked product requirements. None are implemented yet.
          </p>
          <ul className="mt-4 space-y-3">
            {planned.map((text) => (
              <li
                key={text}
                className="flex gap-3 text-sm leading-6 text-zinc-600"
              >
                <span aria-hidden className="select-none text-zinc-400">
                  —
                </span>
                <span>{text}</span>
              </li>
            ))}
          </ul>
        </section>
      </main>

      <footer className="mt-16 flex flex-col gap-2 border-t border-zinc-200 pt-6 text-xs text-zinc-500 sm:flex-row sm:items-center sm:justify-between">
        <p>Phase 01 — repository bootstrap. PayPal sandbox only; no real money.</p>
        <p>MIT licensed</p>
      </footer>
    </div>
  );
}
