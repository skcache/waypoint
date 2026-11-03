// Browser-level test for the three-act case file. It needs a running Next.js
// server and a Chrome/Chromium binary (CHROME_PATH), and drives the real
// interactions: act navigation, ledger entries, the candidate tabs, the unsafe
// patch, the invariant matrix, the sandbox gate, the provider-call stepper, the
// pinned human gate, and the mobile layout.
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
const observations = run.incident?.observations ?? [];
assert.ok(observations.length >= 4, "export contains the F3 provider observations");
const sequences = observations.map((o) => o.seq);
assert.deepEqual(sequences, [...sequences].sort((a, b) => a - b), "observation sequence is ordered");
assert.ok(observations.every((o) => o.tier === "F"), "observations carry their tier provenance");
const capture = observations.find((o) => o.capture_id);
assert.ok(capture?.capture_id, "export contains a captured provider observation");
assert.ok(run.incident.original_order_id && run.incident.verification_order_id, "export keeps both order ids");

for (const [from, to] of [["/trace", "/"], ["/checkout", "/"]]) {
  const r = await fetch(`${base}${from}`, { redirect: "manual" });
  assert.ok(r.status >= 300 && r.status < 400, `${from} redirects away from the retired V1 page`);
  assert.equal(new URL(r.headers.get("location"), base).pathname, to);
}

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
const text = (page, selector) => page.$eval(selector, (el) => el.textContent ?? "");
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
  // Wait for hydration: the candidate tabs must respond to a click.
  await page.waitForFunction(() => Object.keys(document.querySelector('[data-candidate="unsafe"]') ?? {}).some((k) => k.startsWith("__react")), { timeout: 20000 });

  const body = (await page.evaluate(() => document.body.innerText)).toLowerCase();
  assert.ok(body.includes("an ai fixed a payment bug"), "hero states the product in one line");
  assert.ok(body.includes("recorded"), "the page says it is recorded evidence");
  assert.ok(!body.includes("act 1"), "no numbered act kickers");

  // Act navigation scrolls to the act and marks it current.
  await page.click('nav[aria-label="Acts"] a[href="#fix"]');
  await page.waitForFunction(() => document.querySelector('nav[aria-label="Acts"] a[href="#fix"]')?.getAttribute("aria-current") === "location", { timeout: 5000 });

  // BREAK: the ledger statement lists what the oracle read; entries are selectable.
  const entries = await page.$$('[data-testid="ledger-row"]');
  assert.ok(entries.length >= 4, "ledger entries render (capture, status, effects)");
  const ledger = await page.evaluate(() => document.querySelector("#break").innerText);
  assert.ok(ledger.includes("evt-cap-1") && ledger.includes("evt-cap-2"), "both fulfillment effects are listed");
  assert.ok(ledger.includes("does not balance"), "the statement states it does not balance");
  await page.evaluate(() => document.querySelectorAll('[data-testid="ledger-row"]')[3].click());
  await page.waitForFunction(() => document.querySelector('[data-testid="break-detail"]')?.textContent.includes("evt-cap-2"));
  assert.ok((await text(page, '[data-testid="break-detail"]')).includes("I13"), "selecting an effect shows I13");
  await page.evaluate(() => [...document.querySelectorAll('#break [aria-label="Invariants"] button')].find((b) => b.textContent.includes("I6")).click());
  await page.waitForFunction(() => document.querySelector('[data-testid="break-detail"]')?.textContent.includes("Invariant I6"));
  assert.ok(ledger.toLowerCase().includes("cannot show") || (await page.evaluate(() => document.querySelector("#break").innerText)).toLowerCase().includes("what this evidence cannot show"), "evidence limits are stated");
  assert.ok(ledger.includes(run.incident.original_order_id ?? run.incident.order_id), "incident order id is visible");

  // FIX: diagnosis provenance, the real diff, and a note that jumps to a line.
  const fix = (await page.evaluate(() => document.querySelector("#fix").innerText)).toLowerCase();
  assert.ok(fix.includes(run.run.kind === "replay" ? "replayed recorded model output" : "recorded hosted-model reply"), "model provenance is accurate");
  assert.ok(fix.includes("no merchant_webhook_events") && fix.includes("not directly observed"), "the model's uncertainty is visible");
  const diff = await text(page, '#fix [data-testid="demo-diff"]');
  assert.ok(diff.includes("'FULFILL'"), "generated diff is visible");
  const note = await page.$('#fix [aria-label="Reading the patch"] button');
  if (note) {
    await note.click();
    await page.waitForFunction(() => document.querySelector('#fix [aria-label="Reading the patch"] [aria-live]')?.textContent.length > 20);
  }

  // JUDGE: candidate tabs, including the unsafe patch as a primary interaction.
  await page.click('[data-candidate="repair"]');
  await page.waitForSelector('[data-testid="demo-oracle"]');
  const verified = await text(page, '[data-testid="demo-oracle"]');
  assert.ok(verified.includes("VERIFIED"), "repaired candidate verdict is visible");
  assert.ok(verified.toLowerCase().includes("not a claim about live paypal"), "verification scope is explicit");
  await page.click('[data-candidate="unsafe"]');
  await page.waitForSelector('[data-testid="demo-unsafe"]');
  const unsafe = await text(page, '[data-testid="demo-unsafe"]');
  assert.ok(unsafe.includes("UNSAFE") && unsafe.includes("I13"), "unsafe verdict names I13");
  assert.ok(unsafe.includes("Historical independent-oracle evidence"), "historical provenance is explicit");
  if (run.unsafe.fresh_trusted_gate.executed) assert.ok(unsafe.includes("Fresh trusted gate"));
  else assert.ok(unsafe.includes("not run for this recorded artifact"), "missing fresh gate is explicit");
  if (run.unsafe.diff) {
    const unsafeDiff = await text(page, '[data-testid="demo-unsafe-diff"]');
    assert.ok(unsafeDiff.includes("Seeded unsafe candidate"), "the unsafe candidate's own code is shown");
    assert.ok(!unsafeDiff.includes("'FULFILL'"), "the unsafe diff is not the repair");
  }

  // Matrix: differences by default, expandable counterexamples per candidate.
  const matrix = '[data-testid="invariant-matrix"]';
  const diffRows = await page.$$eval(`${matrix} ul > li`, (rows) => rows.length);
  assert.ok(diffRows >= 1 && diffRows < 14, `the default view shows only differing rows (${diffRows})`);
  const matrixText = await text(page, matrix);
  assert.ok(matrixText.includes("evt-cap-1") && matrixText.includes("evt-cap-2"), "the I13 counterexample is expanded");
  await page.evaluate((m) => [...document.querySelectorAll(`${m} button`)].find((b) => /^All 14/.test(b.textContent.trim())).click(), matrix);
  await page.waitForFunction((m) => document.querySelectorAll(`${m} ul > li`).length === 14, {}, matrix);

  // Sandbox gate and provider calls (verification order, stepped).
  const steps = await page.$$eval('[data-testid="gate-steps"] li', (rows) => rows.length);
  assert.equal(steps, run.verification.steps.length, "every recorded sandbox step is listed");
  assert.ok((await page.evaluate(() => document.querySelector("#judge").innerText)).toLowerCase().includes("verification order"), "provider calls are tied to the verification order");
  await page.evaluate(() => document.querySelectorAll('[data-testid="observation-row"]')[0].click());
  await page.evaluate(() => document.querySelector('[aria-label="Next call"]').click());
  await page.waitForFunction(() => document.querySelector('[data-testid="provider-calls"] dl')?.textContent.includes("Request id") || true);
  await page.evaluate(() => document.querySelectorAll('[data-testid="observation-row"]')[document.querySelectorAll('[data-testid="observation-row"]').length - 1].click());
  await page.waitForFunction((id) => document.querySelector('[data-testid="provider-calls"]').innerText.includes(id), {}, capture.capture_id);

  // The hero list opens the judge on the chosen candidate.
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.evaluate(() => document.querySelectorAll('[role="radiogroup"] [role="radio"]')[0].click());
  await page.waitForFunction(() => document.querySelector('[data-candidate="before"]')?.getAttribute("aria-selected") === "true");

  // The human gate is pinned and expands.
  const gate = await page.$('[data-testid="human-gate"]');
  assert.ok(gate, "human gate is rendered");
  const box = await gate.boundingBox();
  assert.ok(box && box.y + box.height >= 990, "human gate is pinned to the bottom of the viewport");
  await page.evaluate(() => document.querySelector('[data-testid="human-gate"] button[aria-expanded]').click());
  await page.waitForSelector('[data-testid="demo-human"]');
  const human = await text(page, '[data-testid="demo-human"]');
  assert.ok(human.includes("No approval has been recorded"), "approval state is explicit");
  assert.ok(/cannot merge, push, deploy, capture, refund or retry/.test(human), "human-review boundary copy is visible");

  const overflow = await page.evaluate(() => ({ s: document.documentElement.scrollWidth, c: document.documentElement.clientWidth }));
  assert.ok(overflow.s <= overflow.c + 1, "desktop page has no horizontal overflow");

  // Mobile at both common widths: no overflow, readable type, reachable navigation.
  for (const width of [390, 375]) {
    await page.setViewport({ width, height: 844, deviceScaleFactor: 1, isMobile: true, hasTouch: true });
    await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector('[data-candidate="unsafe"]');
    const m = await page.evaluate(() => ({
      s: document.documentElement.scrollWidth,
      c: document.documentElement.clientWidth,
      tiny: [...document.querySelectorAll("body *")].filter((e) => [...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim()) && parseFloat(getComputedStyle(e).fontSize) < 12.5).map((e) => e.textContent.trim().slice(0, 30)),
    }));
    assert.ok(m.s <= m.c + 1, `${width}px page has no horizontal overflow`);
    assert.deepEqual(m.tiny, [], `${width}px has no text under 12.5px`);
    const navBox = await (await page.$('nav[aria-label="Acts"] a[href="#judge"]')).boundingBox();
    assert.ok(navBox.height >= 44, "act navigation has a 44px touch target");
  }

  const bench = await browser.newPage();
  bench.on("pageerror", (err) => pageErrors.push(`bench: ${err}`));
  await bench.setViewport({ width: 390, height: 844 });
  await bench.goto(`${base}/benchmark`, { waitUntil: "domcontentloaded" });
  assert.ok((await bench.evaluate(() => document.body.innerText)).includes("held-out incidents"), "the benchmark page is reachable");

  const home = await browser.newPage();
  home.on("pageerror", (err) => pageErrors.push(`home: ${err}`));
  await home.setViewport({ width: 1440, height: 1000 });
  await home.goto(`${base}/`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await home.waitForFunction((id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id, { timeout: 20000 }, recordedRun.run.run_id);
  const homeText = await home.evaluate(() => document.body.innerText);
  assert.ok(homeText.toLowerCase().includes("duplicate fulfillment"), "public entry loads the recorded case");

  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
