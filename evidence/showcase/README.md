# Showcase captures

Actual headless Chrome captures of the committed recorded case (`/`) at desktop
and mobile widths. The incident capture shows the provider-call sequence and the
pre-repair ledger counterexamples separately. The Oracle and unsafe-counterexample
captures use the same recorded data and are not live model or PayPal activity.

| File | Viewport |
| --- | --- |
| `desktop-1440.png` | 1440 × 1000, home entry |
| `mobile-390.png` | 390 × 844, home entry |
| `incident-1440.png` | 1440 × 1000, incident evidence |
| `mobile-incident-390.png` | 390 × 844, incident evidence |
| `oracle-1440.png` | 1440 × 1000, verified oracle stage |
| `unsafe-1440.png` | 1440 × 1000, historical unsafe counterexample |

Captured against the production Next.js build with the committed
`apps/web/public/demo/recorded.json` projection. No database, Docker, model, or
PayPal credential was used for these captures.
