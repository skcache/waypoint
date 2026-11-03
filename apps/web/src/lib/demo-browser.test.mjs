// Browser-level test for the poster case file. It needs a running Next.js server
// and a Chrome/Chromium binary (CHROME_PATH). It proves the page's own claim in a
// real browser: at the surface the AI repair and the unsafe patch render
// identically, and under the judge's lens every tick, effect count and stamp
// matches the export. It also drives the story beats, the patches, the workpaper,
// the sandbox bar, the provider calls, the pinned signature bar and the phone layout.
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
const evals = { before: run.pre_repair, repair: run.oracle, unsafe: run.unsafe };

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const setLens = (page, v) =>
  page.evaluate((val) => {
    const el = document.querySelector('[data-testid="lens"]');
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, String(val));
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }, v);
const ticks = (page, id) =>
  page.$$eval(`[data-testid="${id}"] [data-tick]`, (els) => Object.fromEntries(els.map((e) => [e.getAttribute("data-tick"), e.getAttribute("data-status")])));
const scrollToSel = (page, sel, off = 0) => page.evaluate((s, o) => window.scrollTo(0, document.querySelector(s).getBoundingClientRect().top + scrollY + o), sel, off);

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await page.waitForFunction((id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id, { timeout: 20000 }, run.run.run_id);
  await page.waitForFunction(() => Object.keys(document.querySelector('[data-testid="lens"]') ?? {}).some((k) => k.startsWith("__react")), { timeout: 20000 });
  await page.addStyleTag({ content: "html{scroll-behavior:auto!important}" });

  // BREAK: the product in one line, a receipt that matches the pre-repair verdict, story beats that light its lines.
  const h1 = (await page.$eval("h1", (el) => el.textContent)).toLowerCase();
  assert.ok(h1.includes("one payment") && h1.includes("fulfillments"), "the headline states the incident");
  assert.ok((await page.evaluate(() => document.body.textContent)).toLowerCase().includes("recorded"), "the page says it is recorded evidence");
  assert.ok((await page.evaluate(() => document.body.textContent)).includes("An AI fixed a payment bug. A separate judge checked the ledger."), "the product in one sentence");
  assert.deepEqual(await ticks(page, "receipt-incident"), statusOf(run.pre_repair), "the Break receipt ticks are the pre-repair statuses");
  const rows = await page.$$eval('[data-testid="receipt-incident"] [data-row^="effect"]', (els) => els.length);
  assert.equal(rows, 2, "the incident receipt prints both fulfillment effects");
  await scrollToSel(page, "#beat-1", -200);
  await page.evaluate(() => document.querySelector("#beat-1").nextElementSibling.scrollIntoView({ block: "center" }));
  await wait(900);
  const lit = await page.$$eval('[data-testid="receipt-incident"] .marker[data-on="true"]', (els) => els.length);
  assert.ok(lit >= 1, "a story beat highlights lines on the receipt");
  await page.evaluate(() => document.querySelector('[data-testid="receipt-incident"] [data-row="effect-1"]').click());
  await page.waitForFunction(() => document.querySelector('[data-testid="receipt-note"]')?.textContent.includes("I13"));
  assert.ok((await page.evaluate(() => document.body.textContent)).toLowerCase().includes("what this evidence cannot show"), "evidence limits are stated");
  await page.evaluate(() => document.querySelector('[data-testid="receipt-incident"] [data-row="effect-1"]').click());

  // FIX: provenance, the model's own uncertainty, the exact diff and a note that jumps to a line.
  await scrollToSel(page, "#fix", 0);
  await wait(300);
  const fix = (await page.$eval("#fix", (el) => el.textContent)).toLowerCase();
  assert.ok(fix.includes(run.run.kind === "replay" ? "replayed recorded model output" : "recorded hosted-model reply"), "model provenance is accurate");
  assert.ok(fix.includes("no merchant_webhook_events") && fix.includes("not directly observed"), "the model's uncertainty is visible");
  assert.ok((await page.$eval('[data-testid="demo-diff"]', (el) => el.textContent)).includes("'FULFILL'"), "the generated diff is visible");
  const note = await page.$('[data-testid="demo-diff"] [aria-label="Reading the patch"] button');
  if (note) {
    await note.click();
    await page.waitForFunction(() => document.querySelector('[aria-label="Reading the patch"] [aria-live]')?.textContent.length > 20);
  }

  // JUDGE: identical surfaces, different ledgers.
  await scrollToSel(page, '[data-testid="lens-bar"]', -70);
  await wait(1200);
  // Regression: receipts that scroll into view late must still print (a clipped target never reports as visible).
  for (const col of ["before", "repair", "unsafe"]) {
    await page.waitForFunction(
      (c) => {
        const wrap = document.querySelector(`[data-testid="receipt-${c}"]`)?.closest(".print");
        return wrap?.dataset.state === "in" && getComputedStyle(wrap).clipPath === "inset(0px)";
      },
      { timeout: 5000 },
      col,
    );
  }
  const surface = (col) => page.$eval(`[data-testid="receipt-${col}"] [data-layer="surface"]`, (el) => el.textContent.replace(/\s+/g, " ").trim());
  if (run.unsafe.order_status) {
    assert.equal(await surface("repair"), await surface("unsafe"), "at the surface the AI repair and the unsafe patch are indistinguishable");
  } else {
    // An export from before the unsafe status was projected: say so, never invent it.
    assert.ok((await surface("unsafe")).includes("NOT RECORDED"), "an old export's missing unsafe status is shown as not recorded");
  }
  assert.notEqual(await surface("before"), await surface("repair"), "the original's status visibly rolled back");
  assert.equal(await page.$eval('[data-testid="lens"]', (el) => el.value), "0", "the lens starts at the surface");
  for (const col of ["before", "repair", "unsafe"]) {
    assert.equal(await page.$eval(`[data-testid="receipt-${col}"] [data-stamp]`, (el) => el.getAttribute("data-on")), "false", `${col} is not stamped before the lens is dragged`);
  }
  await setLens(page, 1000);
  await wait(900);
  for (const col of ["before", "repair", "unsafe"]) {
    assert.deepEqual(await ticks(page, `receipt-${col}`), statusOf(evals[col]), `${col}: ledger ticks match the export`);
    const stamp = await page.$eval(`[data-testid="receipt-${col}"] [data-stamp]`, (el) => ({ on: el.getAttribute("data-on"), word: el.getAttribute("data-stamp") }));
    assert.equal(stamp.on, "true", `${col} is stamped once the ledger is read`);
    assert.equal(stamp.word, failing(evals[col]).length ? "Unsafe" : "Verified", `${col} stamp matches the oracle verdict`);
    const fx = await page.$$eval(`[data-testid="receipt-${col}"] [data-row^="effect"]`, (els) => els.length);
    assert.equal(fx, col === "repair" ? 1 : 2, `${col} prints ${col === "repair" ? 1 : 2} fulfillment lines`);
  }
  const failTicks = await page.$$eval('[data-testid="receipt-unsafe"] [data-status="FAIL"]', (els) => els.map((e) => e.getAttribute("data-tick")).sort());
  assert.deepEqual(failTicks, failing(run.unsafe), "the unsafe receipt fails exactly what the oracle failed");
  const outcome = await page.$eval('[data-testid="outcome-unsafe"]', (el) => el.textContent);
  assert.ok(outcome.includes("I13"), "the outcome names the failed invariant");
  const prov = await page.evaluate(() => document.querySelector('[data-testid="provenance-unsafe"]').textContent + document.querySelector('[data-testid="provenance-repair"]').textContent);
  assert.ok(prov.includes("Historical independent-oracle evidence"), "historical provenance is explicit");
  assert.ok(prov.toLowerCase().includes("not a claim about live paypal"), "verification scope is explicit");
  if (run.unsafe.fresh_trusted_gate.executed) assert.ok(prov.includes("Fresh trusted gate"));
  else assert.ok(prov.includes("not run for this recorded artifact"), "a missing fresh gate is explicit");
  await page.evaluate(() => document.querySelector('[data-testid="receipt-unsafe"] [data-row="effect-1"]').click());
  await page.waitForFunction(() => document.querySelector('[data-testid="judge-note"]')?.textContent.includes("I13"));
  // The guided sweep brings the lens back and forth.
  await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Back to the surface").click());
  await page.waitForFunction(() => document.querySelector('[data-testid="lens"]').value === "0", { timeout: 5000 });
  await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Look under the surface").click());
  await page.waitForFunction(() => document.querySelector('[data-testid="lens"]').value === "1000", { timeout: 5000 });
  // The two patches, as code.
  await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Read the two patches").click());
  await page.waitForSelector('[data-testid="judge-diff-repair"]');
  assert.ok((await page.$eval('[data-testid="judge-diff-repair"]', (el) => el.textContent)).includes("'FULFILL'"), "the repair's diff is shown");
  if (run.unsafe.diff) {
    const unsafeDiff = await page.$eval('[data-testid="demo-unsafe-diff"]', (el) => el.textContent);
    assert.ok(unsafeDiff.includes("Seeded unsafe candidate"), "the unsafe candidate's own code is shown");
    assert.ok(!unsafeDiff.includes("'FULFILL'"), "the unsafe diff is not the repair");
  }

  // PROOF: workpaper, sandbox bar, provider calls.
  await scrollToSel(page, "#proof", 0);
  await wait(300);
  const wp = '[data-testid="workpaper"]';
  const differing = Object.keys(statusOf(run.oracle)).filter((id) => new Set([statusOf(run.pre_repair)[id], statusOf(run.oracle)[id], statusOf(run.unsafe)[id]]).size > 1);
  assert.equal(await page.$$eval(`${wp} li`, (els) => els.length >= 1), true);
  assert.equal(await page.$$eval(`${wp} button[data-row]`, (els) => els.length), differing.length, "the workpaper opens on the differences");
  await page.evaluate((w) => [...document.querySelectorAll(`${w} button`)].find((b) => /^All 14/.test(b.textContent.trim())).click(), wp);
  await page.waitForFunction((w) => document.querySelectorAll(`${w} button[data-row]`).length === 14, {}, wp);
  const marks = await page.$$eval(`${wp} [data-mark]`, (els) => Object.fromEntries(els.map((e) => [e.getAttribute("data-mark"), e.getAttribute("data-status")])));
  for (const [col, e] of Object.entries({ before: run.pre_repair, repair: run.oracle, unsafe: run.unsafe })) {
    for (const [id, st] of Object.entries(statusOf(e))) assert.equal(marks[`${id}-${col}`], st, `workpaper ${id}/${col}`);
  }
  const steps = run.verification.steps;
  const widths = await page.$$eval('[data-testid="sandbox"] [role="group"] button', (els) => els.map((e) => ({ n: e.getAttribute("data-step"), w: e.getBoundingClientRect().width })));
  assert.equal(widths.length, steps.length, "one segment per sandbox step");
  const longest = [...steps].sort((a, b) => b.duration_ms - a.duration_ms)[0].name;
  assert.equal([...widths].sort((a, b) => b.w - a.w)[0].n, longest, "the longest step has the widest segment");
  await page.evaluate(() => document.querySelectorAll('[data-testid="observation-row"]')[document.querySelectorAll('[data-testid="observation-row"]').length - 1].click());
  await page.waitForFunction((id) => document.querySelector('[data-testid="call-detail"]')?.textContent.includes(id), {}, capture.capture_id);
  assert.ok((await page.$eval('[data-testid="provider-calls"]', (el) => el.textContent)).toLowerCase().includes("verification order"), "provider calls are tied to the verification order");

  // The signature bar is pinned in every scene and expands.
  for (const sel of ["#break", "#fix", "#judge", "#proof"]) {
    await scrollToSel(page, sel, 0);
    await wait(200);
    const box = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(box && box.y + box.height >= 880, `the signature bar is visible at the foot of ${sel}`);
  }
  await page.evaluate(() => document.querySelector('[data-testid="human-gate"] button[aria-expanded]').click());
  await page.waitForSelector('[data-testid="demo-human"]');
  const human = await page.$eval('[data-testid="demo-human"]', (el) => el.textContent);
  assert.ok(human.includes("No approval has been recorded"), "approval state is explicit");
  assert.ok(/cannot merge, push, deploy, capture, refund or retry/.test(human), "the review-boundary copy is visible");
  const overflow = await page.evaluate(() => ({ s: document.documentElement.scrollWidth, c: document.documentElement.clientWidth }));
  assert.ok(overflow.s <= overflow.c + 1, "desktop page has no horizontal overflow");

  // Phones: no overflow, readable type, 44px targets, a working lens.
  for (const width of [390, 375]) {
    await page.setViewport({ width, height: 844, deviceScaleFactor: 1, isMobile: true, hasTouch: true });
    await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => Object.keys(document.querySelector('[data-testid="lens"]') ?? {}).some((k) => k.startsWith("__react")));
    const m = await page.evaluate(() => ({
      s: document.documentElement.scrollWidth,
      c: document.documentElement.clientWidth,
      tiny: [...document.querySelectorAll("body *")].filter((e) => !e.closest("svg") && [...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim()) && parseFloat(getComputedStyle(e).fontSize) < 12.5).map((e) => e.textContent.trim().slice(0, 30)),
      h1: document.querySelector("h1").getBoundingClientRect().right,
    }));
    assert.ok(m.s <= m.c + 1, `${width}px page has no horizontal overflow`);
    assert.deepEqual(m.tiny, [], `${width}px has no text under 12.5px`);
    assert.ok(m.h1 <= width, `${width}px headline fits`);
    const nav = await (await page.$('nav[aria-label="Sections"] a[href="#judge"]')).boundingBox();
    assert.ok(nav.height >= 44, "section navigation has a 44px touch target");
    const lens = await (await page.$('[data-testid="lens"]')).boundingBox();
    assert.ok(lens.height >= 44, "the lens slider has a 44px touch target");
    await setLens(page, 1000);
    await wait(700);
    assert.deepEqual(await ticks(page, "receipt-unsafe"), statusOf(run.unsafe), `${width}px: the lens works on a phone`);
    const gate = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(gate.y + gate.height >= 830, "the signature bar is pinned on phones");
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

  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
