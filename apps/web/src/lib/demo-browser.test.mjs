// Minimal browser-level smoke test for the /demo page. It requires a running
// Next.js server and a Chrome/Chromium binary (CHROME_PATH). It checks the
// actual rendered run, not just HTTP status or a page title.
import puppeteer from "puppeteer-core";
import assert from "node:assert/strict";

const base = process.env.DEMO_URL;
const chrome = process.env.CHROME_PATH;
if (!base || !chrome) {
  console.error("demo-browser: DEMO_URL and CHROME_PATH are required");
  process.exit(2);
}

const dataPath = process.env.DEMO_DATA === "recorded" ? "/demo/recorded.json" : "/demo/local.json";
const res = await fetch(`${base}${dataPath}`, { cache: "no-store" });
assert.equal(res.status, 200, `${dataPath} must be served`);
const run = await res.json();
assert.ok(run.run?.run_id, "local export has a run id");
assert.ok(run.run?.evidence_layout, "local export declares its evidence layout");
const observations = run.incident?.observations ?? [];
assert.ok(observations.length >= 4, "local export contains the F3 provider observations");
const sequences = observations.map((o) => o.seq);
assert.deepEqual(sequences, [...sequences].sort((a, b) => a - b), "observation sequence is ordered");
assert.equal(new Set(sequences).size, sequences.length, "observation sequence is unique");
assert.ok(observations.every((o) => o.tier === "F"), "observations carry their tier provenance");
const captureID = observations.map((o) => o.capture_id).find(Boolean);
assert.ok(captureID, "local export contains a captured provider observation");

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 1100, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  await page.goto(`${base}/demo`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await page.waitForFunction(
    (id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id,
    { timeout: 20000 },
    run.run.run_id,
  );

  const text = await page.evaluate(() => document.body.innerText);
  const diff = await page.$eval('[data-testid="demo-diff"]', (el) => el.textContent ?? "");
  const human = await page.$eval('[data-testid="demo-human"]', (el) => el.textContent ?? "");
  const unsafe = await page.$eval('[data-testid="demo-unsafe"]', (el) => el.textContent ?? "");
  const oracle = await page.$eval('[data-testid="demo-oracle"]', (el) => el.textContent ?? "");

  assert.ok(text.includes(run.run.run_id), "page renders the run id");
  assert.ok(oracle.includes("VERIFIED_FOR_TESTED_SCENARIOS"), "oracle verdict is visible");
  assert.ok(diff.includes("'FULFILL'"), "generated diff is visible");
  assert.ok(text.includes(captureID.slice(0, 12)), "capture identity is visible");
  assert.ok(unsafe.includes("I13") && unsafe.includes("2 fulfillment effects"), "I13 counterexample is visible");
  assert.ok(human.includes("Candidate patch in disposable worktree") && human.includes("yes"), "candidate worktree fact is visible");
  assert.ok(human.includes("Developer tree applied") && human.includes("no"), "developer tree remains unchanged");
  assert.ok(human.includes("Deployed") && human.includes("no"), "deployment is false");
  assert.ok(human.includes("Human approval required") && human.includes("yes"), "human boundary is visible");
  assert.ok(
    text.includes("No merge, push, deploy, capture, refund, or payment retry exists"),
    "human-review boundary copy is visible",
  );
  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);

  if (process.env.DEMO_SCREENSHOT) {
    await page.screenshot({ path: process.env.DEMO_SCREENSHOT, fullPage: true });
  }
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
