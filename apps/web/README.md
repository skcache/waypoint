# Waypoint — web app

Next.js (App Router) + TypeScript + Tailwind CSS frontend for Waypoint.

The home page (`/`, also `/incidents`, and `/demo` for a local `make demo` export) is one scrolling
poster built from a single sanitized evidence projection (`public/demo/recorded.json`). Its
thesis: **a patch can look fixed and still be wrong.**

- **Break** — the incident order's ledger printed as a receipt; scroll beats light its lines.
- **Fix** — the recorded model diagnosis and the exact generated patch, on a paper slip.
- **Judge** — three receipts (original code, AI repair, seeded unsafe patch) and one lens. At the
  surface the repair and the unsafe patch show the same status; under the lens each ledger, tick
  grid and stamp is the oracle's own record.
- **Proof** — the 14-invariant workpaper, the sandbox bar (segment widths are recorded
  durations) and the provider calls (they belong to the verification order).

The human-review boundary is pinned on every screen as an empty signature line. Nothing here
calls a model, PayPal or a database. `/benchmark` and `/incidents/[id]` are the M5 review console.

Evidence rules: no webhook timeline, effect key or capture id is invented. Receipt lines come
straight from the oracle's counterexamples; an export that lacks a field (for example an older
`local.json`) shows "not recorded". `src/lib/evidence.ts` derives everything and is unit-tested,
including the claim that the repair and unsafe surfaces are identical while their ledgers differ.

- Run: `npm run dev` — or `make dev-web` from the repository root
- Build: `npm run build`
- Lint: `npm run lint`
- Unit tests: `npm test`
- Browser test: `DEMO_URL=http://localhost:3000 CHROME_PATH=/path/to/chrome npm run test:browser`
  against a running `next start`; set `DEMO_DATA=recorded` to use the committed export

Design: Archivo (variable width) for the poster voice, Geist Mono for ids and code only; colour
fields (vermilion alarm, ink, paper) with lime as the auditor's highlighter; no text under 13px;
status is never colour alone; patch annotations are bound to the patch digest.

See the repository root `README.md` for full project context.
