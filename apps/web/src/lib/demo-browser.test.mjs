// Browser-level test for the Waypoint walkthrough. It needs a running Next.js server
// and a Chrome/Chromium binary (CHROME_PATH). It proves the page's own claims in a
// real browser: every tile, count and verdict on the board matches the export, a
// failed rule really is given more space, the AI repair and the seeded unsafe patch
// share one order status while their ledgers differ, the human-review boundary is
// always on screen, the theme toggle persists, and phones have no overflow.
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
const recordedRun = await (await fetch(`${base}/demo/recorded.json`, { cache: "no-store" })).json();
assert.ok(recordedRun.run?.run_id, "recorded export has a run id");
const res = await fetch(`${base}${dataPath}`, { cache: "no-store" });
assert.equal(res.status, 200, `${dataPath} must be served`);
const run = await res.json();
assert.ok(run.run?.run_id, "export has a run id");
const observations = run.incident?.observations ?? [];
assert.ok(observations.length >= 4 && observations.every((o) => o.tier === "F"), "export carries the F3 provider observations with their tier");
const capture = observations.find((o) => o.capture_id);
assert.ok(capture?.capture_id, "export contains a captured provider observation");
assert.ok(run.incident.original_order_id && run.incident.verification_order_id, "export keeps both order ids");

for (const from of ["/trace", "/checkout"]) {
  const r = await fetch(`${base}${from}`, { redirect: "manual" });
  assert.ok(r.status >= 300 && r.status < 400, `${from} redirects away from the retired V1 page`);
  assert.equal(new URL(r.headers.get("location"), base).pathname, "/");
}

const statusOf = (e) => Object.fromEntries(e.invariants.map((i) => [i.id, i.status]));
const failing = (e) => e.invariants.filter((i) => i.status === "FAIL").map((i) => i.id).sort();

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const click = (page, sel) => page.evaluate((s) => document.querySelector(s).click(), sel);
const text = (page, sel) => page.$eval(sel, (el) => el.textContent.replace(/\s+/g, " ").trim());
const tiles = (page) =>
  page.$$eval('[data-testid="board"] [data-tile]', (els) =>
    Object.fromEntries(els.map((e) => { const r = e.getBoundingClientRect(); return [e.getAttribute("data-tile"), { status: e.getAttribute("data-status"), area: r.width * r.height, bg: getComputedStyle(e).backgroundColor }]; })),
  );
const tileStatuses = async (page) => Object.fromEntries(Object.entries(await tiles(page)).map(([k, v]) => [k, v.status]));
const step = async (page, n) => {
  await click(page, `[data-step-row="${n}"]`);
  await page.waitForFunction((k) => document.querySelector('[data-testid="step-count"]')?.textContent.includes(`Step ${k} of`), {}, n);
  await wait(900);
};
const hydrated = (page) => page.waitForFunction(() => Object.keys(document.querySelector('[data-testid="next"]') ?? {}).some((k) => k.startsWith("__react")), { timeout: 20000 });

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await page.waitForFunction((id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id, { timeout: 20000 }, run.run.run_id);
  await hydrated(page);
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "dark", "dark is the default theme");

  // STEP 1, the incident: one lead number, one dominant board, the product in one sentence.
  const body = (await page.evaluate(() => document.body.textContent)).toLowerCase();
  assert.ok(body.includes("run is recorded") || body.includes("run is local"), "the page says what kind of run it shows");
  assert.match(await text(page, '[data-testid="execution-boundary"]'), /explores saved evidence.*make demo.*locally/, "the page distinguishes evidence exploration from local execution");
  assert.ok(body.includes("separate checker tests each repair"), "the product is explained in one sentence");
  assert.ok((await text(page, '[data-testid="case-label"]')).includes("Duplicate fulfillment"), "the case is named");
  assert.equal(await text(page, '[data-testid="hero-value"]'), "2", "the lead number is the incident's fulfillment count");
  assert.equal(await text(page, '[data-testid="fact-fulfilled"]'), "2");
  assert.equal(await text(page, '[data-testid="fact-captured"]'), "1");
  assert.equal(await text(page, '[data-testid="fact-verdict"]'), "Rejected");
  assert.deepEqual(await tileStatuses(page), statusOf(run.pre_repair), "the board shows the pre-repair statuses");
  const t1 = await tiles(page);
  const heldAreas = Object.values(t1).filter((t) => t.status === "PASS").map((t) => t.area);
  const meanHeld = heldAreas.reduce((a, b) => a + b, 0) / heldAreas.length;
  for (const id of failing(run.pre_repair)) assert.ok(t1[id].area / meanHeld > 2.5, `${id} failed, so it takes much more room than a rule that held`);
  await click(page, '[data-tile="I13"]');
  await page.waitForFunction(() => document.querySelector('[data-testid="inspector"]')?.textContent.includes("I13"));
  const insp = await text(page, '[data-testid="inspector"]');
  assert.ok(insp.includes("2 fulfillment effects") && insp.includes("evt-cap-1"), "the inspector shows the oracle's counterexample");
  assert.ok((await text(page, '[data-testid="provenance"]')).includes("before the repair"), "provenance is stated");

  // STEP 2, the AI repair: the exact patch and the sandbox it ran in.
  await step(page, 2);
  assert.equal(await text(page, '[data-testid="hero-value"]'), `+${run.patch.diff.split("\n").filter((l) => l.startsWith("+") && !l.startsWith("+++")).length}`);
  const fix = (await page.$eval('[aria-label="Walkthrough"]', (el) => el.textContent)).toLowerCase();
  assert.ok(fix.includes(run.run.kind === "replay" ? "replay of a recorded model reply" : "recorded reply from a hosted model"), "model provenance is accurate");
  assert.ok(fix.includes("no merchant_webhook_events") && fix.includes("not directly observed"), "the model's uncertainty is visible");
  assert.ok((await page.$eval('[data-testid="demo-diff"]', (el) => el.textContent)).includes("'FULFILL'"), "the generated diff is visible");
  const noteBtn = await page.$('[data-testid="demo-diff"] [aria-label="Notes on the patch"] button');
  if (noteBtn) {
    await noteBtn.click();
    await page.waitForFunction(() => document.querySelector('[aria-label="Notes on the patch"] [aria-live]')?.textContent.length > 20);
  }
  const steps = run.verification.steps;
  const widths = await page.$$eval('[data-testid="sandbox"] [role="group"] button', (els) => els.map((e) => ({ n: e.getAttribute("data-step"), w: e.getBoundingClientRect().width })));
  assert.equal(widths.length, steps.length, "one segment per sandbox step");
  const longest = [...steps].sort((a, b) => b.duration_ms - a.duration_ms)[0].name;
  assert.equal([...widths].sort((a, b) => b.w - a.w)[0].n, longest, "the longest step has the widest segment");

  // STEP 3, the independent check: lit board for the accepted repair, one huge failed tile for the unsafe patch.
  await step(page, 3);
  assert.equal(await text(page, '[data-testid="hero-value"]'), "1", "one of two candidate patches is accepted");
  assert.equal(await text(page, '[data-testid="candidate-facts"] [data-testid="fact-verdict"]'), "Accepted");
  assert.deepEqual(await tileStatuses(page), statusOf(run.oracle), "the AI repair board matches the oracle");
  const repairTiles = await tiles(page);
  const heldBg = new Set(Object.values(repairTiles).filter((t) => t.status === "PASS").map((t) => t.bg));
  assert.equal(heldBg.size, 1, "all held rules share one tone");
  const repairFacts = { status: await text(page, '[data-testid="fact-status"]'), fulfilled: await text(page, '[data-testid="fact-fulfilled"]') };
  assert.equal(repairFacts.fulfilled, "1");
  assert.ok((await text(page, '[data-testid="provenance"]')).toLowerCase().includes("no claim about live paypal"), "verification scope is explicit");

  await click(page, '[data-testid="candidate-unsafe"]');
  await page.waitForFunction(() => document.querySelector('[data-testid="candidate-facts"]')?.getAttribute("data-candidate") === "unsafe");
  await wait(900);
  assert.deepEqual(await tileStatuses(page), statusOf(run.unsafe), "the unsafe board matches the oracle");
  const unsafeTiles = await tiles(page);
  assert.deepEqual(Object.keys(unsafeTiles).filter((k) => unsafeTiles[k].status === "FAIL").sort(), failing(run.unsafe), "the unsafe patch fails exactly what the oracle failed");
  assert.equal(await text(page, '[data-testid="fact-fulfilled"]'), "2");
  assert.equal(await text(page, '[data-testid="fact-verdict"]'), "Rejected");
  const unsafeStatus = await text(page, '[data-testid="fact-status"]');
  if (run.unsafe.order_status) {
    assert.equal(unsafeStatus, repairFacts.status, "at the order level the unsafe patch looks exactly like the AI repair");
    const note = await text(page, '[data-testid="surface-note"]');
    assert.ok(note.includes("The ledger is not the same") && note.includes("2 fulfillments"), "the page says the ledger is what tells them apart");
  } else {
    assert.equal(unsafeStatus, "not recorded", "an old export's missing unsafe status is shown as not recorded");
  }
  const prov = await text(page, '[data-testid="provenance"]');
  assert.ok(prov.includes("Historical oracle evidence"), "historical provenance is explicit");
  if (run.unsafe.fresh_trusted_gate.executed) assert.ok(prov.includes("Fresh trusted gate"));
  else assert.ok(prov.includes("did not run for this record"), "a missing fresh gate is explicit");
  await click(page, '[data-tile="I13"]');
  await page.waitForFunction(() => document.querySelector('[data-testid="inspector"]')?.textContent.includes("I13"));

  // STEP 4, human review.
  await step(page, 4);
  const human = await text(page, '[data-testid="demo-human"]');
  assert.ok(human.includes("No person approved this patch"), "approval state is explicit");
  assert.ok(/cannot merge, push, deploy, capture, refund, or retry/.test(human), "the review-boundary copy is visible");
  assert.ok(human.includes("Accepted for human review") && human.includes("disposable worktree"), "the review facts come from the export");
  assert.equal(await text(page, '[data-testid="hero-value"]'), "1", "one patch is waiting for review");

  // The human-review boundary stays on screen at every step.
  for (const n of [1, 2, 3, 4]) {
    await step(page, n);
    const box = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(box && box.y >= 0 && box.y + box.height <= 900, `the review boundary is on screen at step ${n}`);
    assert.ok((await text(page, '[data-testid="human-gate"]')).toLowerCase().includes("no person approved"), "the boundary states there is no approval");
  }

  // Evidence drawer: rule by rule, provider calls, three orders.
  await page.evaluate(() => [...document.querySelectorAll("header button")].find((b) => b.textContent.trim() === "Evidence").click());
  await page.waitForSelector('[data-testid="evidence-drawer"]');
  const wp = '[data-testid="workpaper"]';
  const differing = Object.keys(statusOf(run.oracle)).filter((id) => new Set([statusOf(run.pre_repair)[id], statusOf(run.oracle)[id], statusOf(run.unsafe)[id]]).size > 1);
  assert.equal(await page.$$eval(`${wp} button[data-row]`, (els) => els.length), differing.length, "the workpaper opens on the differences");
  await page.evaluate((w) => [...document.querySelectorAll(`${w} button`)].find((b) => /^All 14/.test(b.textContent.trim())).click(), wp);
  await page.waitForFunction((w) => document.querySelectorAll(`${w} button[data-row]`).length === 14, {}, wp);
  const marks = await page.$$eval(`${wp} [data-mark]`, (els) => Object.fromEntries(els.map((e) => [e.getAttribute("data-mark"), e.getAttribute("data-status")])));
  for (const [col, e] of Object.entries({ before: run.pre_repair, repair: run.oracle, unsafe: run.unsafe })) {
    for (const [id, st] of Object.entries(statusOf(e))) assert.equal(marks[`${id}-${col}`], st, `workpaper ${id}/${col}`);
  }
  const obsRows = await page.$$('[data-testid="observation-row"]');
  await obsRows[obsRows.length - 1].click();
  await page.waitForFunction((id) => document.querySelector('[data-testid="call-detail"]')?.textContent.includes(id), {}, capture.capture_id);
  assert.ok((await text(page, '[data-testid="provider-calls"]')).toLowerCase().includes("verification order"), "provider calls are tied to the verification order");
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector('[data-testid="evidence-drawer"]'));

  const overflow = await page.evaluate(() => ({ s: document.documentElement.scrollWidth, c: document.documentElement.clientWidth }));
  assert.ok(overflow.s <= overflow.c + 1, "desktop page has no horizontal overflow");

  // Theme: the toggle switches and the choice survives a reload.
  await click(page, 'button[aria-label="Switch to light mode"]');
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "light");
  await page.reload({ waitUntil: "domcontentloaded" });
  await hydrated(page);
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "light", "the theme choice persists");
  await click(page, 'button[aria-label="Switch to dark mode"]');
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "dark");

  // Phones: no overflow, readable type, 44px targets, a usable board, the boundary pinned.
  for (const width of [390, 375]) {
    await page.setViewport({ width, height: 844, deviceScaleFactor: 1, isMobile: true, hasTouch: true });
    await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded" });
    await hydrated(page);
    await wait(700);
    for (const n of [1, 3]) {
      if (n === 3) {
        await click(page, '[data-step-row="3"]');
        await wait(900);
      }
      const m = await page.evaluate(() => ({
        s: document.documentElement.scrollWidth,
        c: document.documentElement.clientWidth,
        tiny: [...document.querySelectorAll("body *")].filter((e) => !e.closest("svg") && [...e.childNodes].some((x) => x.nodeType === 3 && x.textContent.trim()) && parseFloat(getComputedStyle(e).fontSize) < 12.5).map((e) => e.textContent.trim().slice(0, 30)),
      }));
      assert.ok(m.s <= m.c + 1, `${width}px step ${n} has no horizontal overflow`);
      assert.deepEqual(m.tiny, [], `${width}px step ${n} has no text under 12.5px`);
    }
    for (const sel of ['[data-testid="next"]', 'button[aria-label="Back"]', '[data-step-row="2"]']) {
      assert.ok((await (await page.$(sel)).boundingBox()).height >= 44, `${sel} has a 44px touch target at ${width}px`);
    }
    assert.deepEqual(await tileStatuses(page), statusOf(run.oracle), `${width}px: the board renders every rule`);
    const gate = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(gate.y + gate.height <= 844 && gate.y > 600, `${width}px: the review boundary is pinned to the foot of the screen`);
    await click(page, '[data-testid="candidate-unsafe"]');
    await wait(900);
    assert.deepEqual(await tileStatuses(page), statusOf(run.unsafe), `${width}px: switching versions works on a phone`);
  }

  const bench = await browser.newPage();
  bench.on("pageerror", (err) => pageErrors.push(`bench: ${err}`));
  await bench.setViewport({ width: 390, height: 844 });
  await bench.goto(`${base}/benchmark`, { waitUntil: "domcontentloaded" });
  assert.ok((await bench.evaluate(() => document.body.innerText)).includes("held-out incidents"), "the benchmark page is reachable");

  const home = await browser.newPage();
  home.on("pageerror", (err) => pageErrors.push(`home: ${err}`));
  await home.setViewport({ width: 1440, height: 900 });
  await home.goto(`${base}/`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await home.waitForFunction((id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id, { timeout: 20000 }, recordedRun.run.run_id);
  assert.ok((await home.evaluate(() => document.body.textContent)).toLowerCase().includes("duplicate fulfillment"), "public entry loads the recorded case");
  assert.match(await text(home, '[data-testid="execution-boundary"]'), /explores saved evidence.*make demo.*locally/);

  // An older export can describe a payment without exporting its capture id.
  // The UI must show unknown for every candidate, even with that prose present.
  const missing = structuredClone(run);
  missing.run.run_id = "missing-capture-proof";
  for (const field of ["pre_repair", "oracle", "unsafe"]) {
    delete missing[field].provider_capture_id;
    missing[field].reason = "2 fulfillment effects for one payment";
  }
  const unknown = await browser.newPage();
  unknown.on("pageerror", (err) => pageErrors.push(`unknown: ${err}`));
  await unknown.setViewport({ width: 390, height: 844 });
  await unknown.setRequestInterception(true);
  unknown.on("request", (req) => {
    if (new URL(req.url()).pathname === "/demo/local.json") {
      void req.respond({ status: 200, contentType: "application/json", body: JSON.stringify(missing) });
    } else {
      void req.continue();
    }
  });
  await unknown.goto(`${base}/demo`, { waitUntil: "domcontentloaded" });
  await unknown.waitForFunction(() => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === "missing-capture-proof");
  assert.equal(await text(unknown, '[data-testid="fact-captured"]'), "not recorded");
  await step(unknown, 3);
  for (const candidate of ["repair", "unsafe"]) {
    await click(unknown, `[data-testid="candidate-${candidate}"]`);
    assert.equal(await text(unknown, '[data-testid="fact-captured"]'), "not recorded", `${candidate}: no capture count is inferred from prose`);
  }

  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
