// Browser-level smoke test for the showcase case file. It requires a running
// Next.js server and a Chrome/Chromium binary (CHROME_PATH). It exercises the
// actual stage switches and candidate switch, not just HTTP status.
import puppeteer from "puppeteer-core";
import assert from "node:assert/strict";

const base = process.env.DEMO_URL;
const chrome = process.env.CHROME_PATH;
if (!base || !chrome) {
  console.error("demo-browser: DEMO_URL and CHROME_PATH are required");
  process.exit(2);
}

const forceRecorded = process.env.DEMO_DATA === "recorded";
const dataPath = forceRecorded ? "/demo/recorded.json" : "/demo/local.json";
const pagePath = forceRecorded ? "/demo?source=recorded" : "/demo";
const recordedRes = await fetch(`${base}/demo/recorded.json`, { cache: "no-store" });
assert.equal(recordedRes.status, 200, "/demo/recorded.json must be served");
const recordedRun = await recordedRes.json();
assert.ok(recordedRun.run?.run_id, "recorded export has a run id");
const res = await fetch(`${base}${dataPath}`, { cache: "no-store" });
assert.equal(res.status, 200, `${dataPath} must be served`);
const run = await res.json();
assert.ok(run.run?.run_id, "export has a run id");
assert.ok(run.run?.evidence_layout, "export declares its evidence layout");
const observations = run.incident?.observations ?? [];
assert.ok(observations.length >= 4, "export contains the F3 provider observations");
const sequences = observations.map((o) => o.seq);
assert.deepEqual(sequences, [...sequences].sort((a, b) => a - b), "observation sequence is ordered");
assert.equal(new Set(sequences).size, sequences.length, "observation sequence is unique");
assert.ok(observations.every((o) => o.tier === "F"), "observations carry their tier provenance");
const capture = observations.find((o) => o.capture_id);
assert.ok(capture?.capture_id, "export contains a captured provider observation");
assert.ok(run.incident.original_order_id, "export preserves the original order id");
assert.ok(run.incident.verification_order_id, "export preserves the verification order id");

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 1000, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await page.waitForFunction(
    (id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id,
    { timeout: 20000 },
    run.run.run_id,
  );

  const text = await page.evaluate(() => document.body.innerText);
  const lower = text.toLowerCase();
  assert.ok(lower.includes("original incident order"), "original order is labelled");
  assert.ok(lower.includes("verification order"), "verification order is labelled");
  assert.ok(
    text.includes(run.incident.original_order_id ?? run.incident.order_id),
    "original order id is visible",
  );
  assert.ok(text.includes(run.incident.verification_order_id), "verification order id is visible");

  // Select the capture event, then inspect its detail.
  const rows = await page.$$('[data-testid="observation-row"]');
  assert.ok(rows.length >= 4, "provider event rows render");
  await rows[rows.length - 1].click();
  await page.waitForFunction(
    (captureId) => document.body.innerText.includes(captureId),
    { timeout: 5000 },
    capture.capture_id,
  );

  await page.click('[data-stage="diagnosis"]');
  await page.waitForFunction(() => document.body.innerText.includes("recorded hosted-model diagnosis"));
  assert.ok((await page.evaluate(() => document.body.innerText)).includes("Recorded hosted-model reply"));

  await page.click('[data-stage="patch"]');
  const diff = await page.$eval('[data-testid="demo-diff"]', (el) => el.textContent ?? "");
  assert.ok(diff.includes("'FULFILL'"), "generated diff is visible");

  await page.click('[data-stage="verification"]');
  const execution = (await page.evaluate(() => document.body.innerText)).toLowerCase();
  assert.ok(execution.includes("disposable sandbox"), "sandbox evidence is visible");
  assert.ok(execution.includes("judge"), "judge step is visible");

  await page.click('[data-stage="oracle"]');
  const oracle = await page.$eval('[data-testid="demo-oracle"]', (el) => el.textContent ?? "");
  assert.ok(oracle.includes("VERIFIED"), "oracle verdict is visible");
  assert.ok(oracle.includes("I13"), "verified invariant list is visible");

  await page.evaluate(() => {
    const button = [...document.querySelectorAll("button")].find(
      (candidate) => candidate.textContent?.trim() === "Unsafe seed",
    );
    button?.click();
  });
  await page.waitForSelector('[data-testid="demo-unsafe"]');
  const unsafe = await page.$eval('[data-testid="demo-unsafe"]', (el) => el.textContent ?? "");
  assert.ok(unsafe.includes("I13") && unsafe.includes("2 fulfillment effects"), "I13 counterexample is visible");
  assert.ok(unsafe.includes("Historical independent-oracle evidence"), "historical provenance is explicit");
  if (run.unsafe.fresh_trusted_gate.executed) {
    assert.ok(unsafe.includes("Fresh trusted gate"), "fresh gate is labelled when executed");
  } else {
    assert.ok(unsafe.includes("not run for this recorded artifact"), "missing fresh gate is explicit");
  }

  await page.click('[data-stage="review"]');
  const human = await page.$eval('[data-testid="demo-human"]', (el) => el.textContent ?? "");
  assert.ok(human.includes("Disposable worktree only"), "candidate worktree fact is visible");
  assert.ok(human.includes("Unchanged"), "developer tree is unchanged");
  assert.ok(human.includes("Not performed"), "deployment is false");
  assert.ok(human.includes("Still required"), "human approval is required");
  assert.ok(
    human.includes("No merge, push, deploy, capture, refund, or payment retry exists"),
    "human-review boundary copy is visible",
  );

  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  assert.ok(overflow.scrollWidth <= overflow.clientWidth + 1, "desktop page has no horizontal overflow");

  await page.setViewport({ width: 390, height: 844, deviceScaleFactor: 1 });
  await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-stage="incident"]');
  const mobileOverflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  assert.ok(mobileOverflow.scrollWidth <= mobileOverflow.clientWidth + 1, "mobile page has no horizontal overflow");
  await page.click('a[href="/benchmark"]');
  await page.waitForFunction(() => document.body.innerText.includes("held-out incidents"));
  assert.ok((await page.evaluate(() => document.body.innerText)).includes("Benchmark"), "mobile navigation reaches the benchmark");

  const home = await browser.newPage();
  home.on("pageerror", (err) => pageErrors.push(`home: ${err}`));
  await home.setViewport({ width: 1440, height: 1000, deviceScaleFactor: 1 });
  await home.goto(`${base}/`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await home.waitForFunction(
    (id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id,
    { timeout: 20000 },
    recordedRun.run.run_id,
  );
  const homeText = await home.evaluate(() => document.body.innerText);
  assert.ok(homeText.includes("Case file"), "public entry exposes the case file");
  assert.ok(homeText.includes("Duplicate fulfillment"), "public entry loads recorded evidence");

  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);
  if (process.env.DEMO_SCREENSHOT) {
    await page.screenshot({ path: process.env.DEMO_SCREENSHOT, fullPage: true });
  }
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
