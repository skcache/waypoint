# Vercel deployment

The public site is the Next.js app in `apps/web`. It renders committed sanitized
evidence and does not require PostgreSQL, Docker, a hosted model, or PayPal
credentials.

## Project settings

| Setting | Value |
| --- | --- |
| Repository | `skcache/waypoint` |
| Root Directory | `apps/web` |
| Framework Preset | Next.js |
| Install Command | `npm ci` |
| Build Command | `npm run build` |
| Output Directory | `.next` (framework default) |
| Node.js | 22.x (repo pins `22.23.3` in `apps/web/.nvmrc`) |

The root directory is **`apps/web`**, not the repository root. Vercel therefore
installs and builds from the app's own `package.json` and `package-lock.json`.

If the project is intentionally configured from the repository root instead,
use:

```text
Install Command: npm --prefix apps/web ci
Build Command:   npm --prefix apps/web run build
Output Directory: apps/web/.next
```

The Root Directory setting is the supported and tested configuration.

## Environment

No environment variables are required for the public preview. In particular:

- Do **not** set `DATABASE_URL`; the public routes do not use PostgreSQL.
- Do **not** set `LLM_*`; the public view never calls a hosted model.
- Do **not** set PayPal credentials; the public view never calls PayPal.
- Leave `WAYPOINT_REVIEW_TOKEN` unset. Review recording is disabled by default;
  the public page is read-only. Do not enable local review writes on a public
  deployment.

The case file at `/` uses the committed, sanitized
`apps/web/public/demo/recorded.json`. `/demo` prefers a fresh local export when
one exists and otherwise falls back to that committed file.

## Local production check

```bash
cd apps/web
npm ci
npm run build
```

Expected: the production build completes and emits `/` and `/demo` as routes.
No database or Docker startup is required for the public paths.

## What not to expose

Do not add a Vercel API route that runs Docker, a repair candidate, a hosted
model, or a PayPal mutation. The existing review endpoint is local-only and
default-closed; the public experience is evidence exploration, not execution.
