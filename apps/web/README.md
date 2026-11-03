# Waypoint — web app

Next.js (App Router) + TypeScript + Tailwind CSS frontend for Waypoint.

The home page (`/`, also `/incidents`, and `/demo` for a local `make demo` export) is one screen:
a quiet rail on the left and a map on the right, built from one sanitized evidence projection
(`public/demo/recorded.json`). The view is kept in the URL hash (`#break`, `#fix`, `#judge`,
`#evidence`).

- **Break** — the incident order's ledger statement, and a map with one cell per financial invariant.
- **Fix** — the recorded model diagnosis and the exact generated patch, on a code sheet.
- **Judge** — the same map re-toned for the original code, the AI repair and a seeded unsafe patch;
  a differences view; each candidate's code.
- **Evidence** — the sandbox checks as a map whose cell areas are their recorded durations, and the
  provider calls (they belong to the verification order).

The human-review boundary is pinned on every screen. Nothing here calls a model, PayPal or a
database. `/benchmark` and `/incidents/[id]` are the M5 review console.

Map geometry is a small power-diagram Voronoi treemap (`src/lib/voronoi.ts`, unit-tested): cells
tile the canvas and follow their target areas. Invariant cells are equal on purpose; tone, not
size, carries the verdict. The sandbox map is area-true.

- Run: `npm run dev` — or `make dev-web` from the repository root
- Build: `npm run build`
- Lint: `npm run lint`
- Unit tests: `npm test` (evidence derivations, diff parsing, geometry, review guard)
- Browser test: `DEMO_URL=http://localhost:3000 CHROME_PATH=/path/to/chrome npm run test:browser`
  against a running `next start`; set `DEMO_DATA=recorded` to use the committed export

Design constraints worth keeping: warm charcoal with tonal cells and one terracotta signal for
"the judge found a failure"; no text under 13px; monospace only for ids and code; status is never
colour alone; any annotation on a patch is bound to that patch's digest.

See the repository root `README.md` for full project context.
