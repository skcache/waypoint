# Waypoint — web app

Next.js (App Router) + TypeScript + Tailwind CSS frontend for Waypoint.

The home page (`/`, also `/incidents`, and `/demo` for a local `make demo` export) is a
three-act case file built from one sanitized evidence projection
(`public/demo/recorded.json`):

1. **Break** — the incident order's ledger statement and the invariants it fails.
2. **Fix** — the recorded model diagnosis and the exact generated patch.
3. **Judge** — the verdict for the original code, the AI repair and a seeded unsafe patch,
   their code, an invariant matrix, the sandbox gate and the verification-order provider calls.

The human-review boundary is pinned on every screen. Nothing on these pages calls a model,
PayPal or a database. `/benchmark` and `/incidents/[id]` are the M5 review console.

- Run: `npm run dev` — or `make dev-web` from the repository root
- Build: `npm run build`
- Lint: `npm run lint`
- Unit tests: `npm test` (evidence derivations, diff parsing, review guard)
- Browser test: `DEMO_URL=http://localhost:3000 CHROME_PATH=/path/to/chrome npm run test:browser`
  against a running `next start`; set `DEMO_DATA=recorded` to use the committed export

Design constraints worth keeping: no text under 13px, monospace only for ids and code, status is
never colour alone, and any annotation on a patch is bound to that patch's digest.

See the repository root `README.md` for full project context.
