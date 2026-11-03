# Waypoint — web app

Next.js (App Router) + TypeScript + Tailwind CSS frontend for Waypoint.

The home page (`/`, also `/incidents`, and `/demo` for a local `make demo` export) is a
four-step walkthrough built from a single sanitized evidence projection
(`public/demo/recorded.json`). It is one screen: a quiet rail on the left leads with one number
and shows where you are; a stage fills the rest with one dominant visual.

1. **The incident** — lead number: the fulfillments the ledger recorded for one payment. The
   stage is the **board**: one tile per money rule the checker tests (14).
2. **The AI repair** — lead number: lines changed. The stage is the exact generated patch and the
   sandbox run it was tested in (segment widths are recorded durations).
3. **The independent check** — lead number: patches accepted (1 of 2). Pick a version of the code
   (original, AI repair, seeded unsafe patch) and the board re-flows to that candidate's verdict.
4. **Human review** — nothing is approved, merged or deployed; the boundary is also pinned on
   every step.

On the board, area and tone carry meaning. A failed rule takes four times the room of a rule that
held, and carries the only attention colour; a rule that does not apply is dotted; an accepted
candidate is the only one that lights up. The AI repair and the seeded unsafe patch end with the
same order status; the unsafe patch's ledger still holds two fulfillments, and the page says so.
The **Evidence** drawer holds the rule-by-rule comparison, the verification-order provider calls,
the three order identities and the sandbox isolation. Nothing here calls a model, PayPal or a
database. `/benchmark` and `/incidents/[id]` are the M5 review console.

Evidence rules: no webhook timeline, effect key or capture id is invented. Everything on the
board comes from the oracle's own records; an export that lacks a field (for example an older
`local.json`) shows "not recorded". `src/lib/evidence.ts` derives it and is unit-tested, as is the
strip-treemap layout (`src/lib/treemap.ts`).

- Run: `npm run dev` — or `make dev-web` from the repository root
- Build: `npm run build`
- Lint: `npm run lint`
- Unit tests: `npm test`
- Browser test: `DEMO_URL=http://localhost:3000 CHROME_PATH=/path/to/chrome npm run test:browser`
  against a running `next start`; set `DEMO_DATA=recorded` to use the committed export

Design: dark by default with a light toggle (`html[data-theme]`, remembered per browser). The
palette borrows PayPal's: navy ground, PayPal blue for what the checker accepted and its yellow for
what failed (a colour inspiration for the PayPal hackathon, not affiliation or endorsement).
Atkinson Hyperlegible Next for text and the lead number (it keeps I, l and 1 apart), Geist Mono for rule ids and code; no text under 12.5px;
status is never colour alone; patch annotations are bound to the patch digest.

See the repository root `README.md` for full project context.
