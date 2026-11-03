// Browser-level test for the map-and-rail case file. It needs a running Next.js
// server and a Chrome/Chromium binary (CHROME_PATH), and drives real
// interactions: view navigation, map cells (checked against the export's own
// invariant statuses), the candidate switch, the unsafe patch's code, the
// time map, the provider calls, the pinned human gate, keyboard use, retired
// routes, and the phone layout.
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
assert.ok(observations.length >= 4, "export contains the F3 provider observations");
assert.ok(observations.every((o) => o.tier === "F"), "observations carry their tier provenance");
const capture = observations.find((o) => o.capture_id);
assert.ok(capture?.capture_id, "export contains a captured provider observation");
assert.ok(run.incident.original_order_id && run.incident.verification_order_id, "export keeps both order ids");

for (const from of ["/trace", "/checkout"]) {
  const r = await fetch(`${base}${from}`, { redirect: "manual" });
  assert.ok(r.status >= 300 && r.status < 400, `${from} redirects away from the retired V1 page`);
  assert.equal(new URL(r.headers.get("location"), base).pathname, "/");
}

const statusesOf = (e) => Object.fromEntries(e.invariants.map((i) => [i.id, i.status]));
const failing = (e) => e.invariants.filter((i) => i.status === "FAIL").map((i) => i.id).sort();

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
const text = (page, selector) => page.$eval(selector, (el) => el.textContent ?? "");
const go = async (page, view) => {
  await page.evaluate((v) => document.querySelector(`nav[aria-label="Views"] a[href="#${v}"]`).click(), view);
  await page.waitForFunction((v) => document.querySelector("main")?.getAttribute("data-view") === v, {}, view);
  await new Promise((r) => setTimeout(r, 450));
};
const mapStatuses = (page) =>
  page.$$eval('[data-testid="verdict-map"] [data-invariant]', (cells) => Object.fromEntries(cells.map((c) => [c.getAttribute("data-invariant"), c.getAttribute("data-status")])));

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 1000, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded", timeout: 20000 });
  await page.waitForFunction((id) => document.querySelector('[data-testid="demo-run-id"]')?.textContent?.trim() === id, { timeout: 20000 }, run.run.run_id);
  await page.waitForSelector('[data-testid="verdict-map"] [data-invariant]', { timeout: 20000 });

  // BREAK: the product in one line, and a map that matches the pre-repair verdict.
  const lower = (await page.evaluate(() => document.body.innerText)).toLowerCase();
  assert.ok(lower.includes("an ai fixed a payment bug"), "the page states the product in one line");
  assert.ok(lower.includes("recorded"), "the page says it is recorded evidence");
  let cells = await mapStatuses(page);
  assert.equal(Object.keys(cells).length, run.pre_repair.invariants.length, "one cell per invariant");
  assert.deepEqual(cells, statusesOf(run.pre_repair), "the Break map shows the pre-repair statuses");
  assert.deepEqual(failing(run.pre_repair).every((id) => cells[id] === "FAIL"), true);
  const ledger = await page.evaluate(() => document.body.innerText);
  assert.ok(ledger.includes("evt-cap-1") && ledger.includes("evt-cap-2"), "both fulfillment effects are listed");
  assert.ok(ledger.includes("does not balance"), "the statement says it does not balance");
  await page.evaluate(() => document.querySelectorAll('[data-testid="ledger-row"]')[3].click());
  await page.waitForFunction(() => document.querySelector('[data-testid="invariant-detail"]')?.closest("section")?.textContent.includes("I13"));
  await page.evaluate(() => document.querySelector('[data-invariant="I6"]').dispatchEvent(new MouseEvent("click", { bubbles: true })));
  await page.waitForFunction(() => document.querySelector('[data-testid="invariant-detail"]')?.closest("section")?.textContent.includes("I6"));
  // Keyboard: a focused cell activates with Enter.
  await page.evaluate(() => document.querySelector('[data-invariant="I8"]').focus());
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => document.querySelector('[data-invariant="I8"]')?.getAttribute("aria-pressed") === "true");
  assert.ok(ledger.toLowerCase().includes("what this evidence cannot show"), "evidence limits are stated");

  // FIX: provenance, uncertainty, the exact diff, and a note that jumps to a line.
  await go(page, "fix");
  const fix = (await page.evaluate(() => document.body.innerText)).toLowerCase();
  assert.ok(fix.includes(run.run.kind === "replay" ? "replayed recorded model output" : "recorded hosted-model reply"), "model provenance is accurate");
  assert.ok(fix.includes("no merchant_webhook_events") && fix.includes("not directly observed"), "the model's uncertainty is visible");
  assert.ok((await text(page, '[data-testid="demo-diff"]')).includes("'FULFILL'"), "generated diff is visible");
  const note = await page.$('[aria-label="Reading the patch"] button');
  if (note) {
    await note.click();
    await page.waitForFunction(() => document.querySelector('[aria-label="Reading the patch"] [aria-live]')?.textContent.length > 20);
  }

  // JUDGE: the map re-tones per candidate; unsafe fails only what the oracle says.
  await go(page, "judge");
  await page.click('[data-testid="candidate-repair"]');
  await page.waitForFunction((ids) => ids.every((id) => document.querySelector(`[data-invariant="${id}"]`)?.getAttribute("data-status") === "PASS"), {}, failing(run.pre_repair));
  cells = await mapStatuses(page);
  assert.deepEqual(cells, statusesOf(run.oracle), "the repaired candidate's map matches the oracle");
  assert.ok((await text(page, '[data-testid="demo-oracle"]')).includes("Verified"), "verdict word is shown");
  assert.ok((await page.evaluate(() => document.body.innerText)).toLowerCase().includes("not a claim about live paypal"), "verification scope is explicit");
  await page.click('[data-testid="candidate-unsafe"]');
  await page.waitForFunction((ids) => ids.every((id) => document.querySelector(`[data-invariant="${id}"]`)?.getAttribute("data-status") === "FAIL"), {}, failing(run.unsafe));
  cells = await mapStatuses(page);
  assert.deepEqual(cells, statusesOf(run.unsafe), "the unsafe candidate's map matches the oracle");
  assert.deepEqual(Object.keys(cells).filter((id) => cells[id] === "FAIL").sort(), failing(run.unsafe));
  const judgeText = await page.evaluate(() => document.body.innerText);
  assert.ok(judgeText.includes("Historical independent-oracle evidence"), "historical provenance is explicit");
  if (run.unsafe.fresh_trusted_gate.executed) assert.ok(judgeText.includes("Fresh trusted gate"));
  else assert.ok(judgeText.includes("not run for this recorded artifact"), "missing fresh gate is explicit");
  // Differences highlight dims every cell on which the candidates agree.
  await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.startsWith("Show differences")).click());
  await new Promise((r) => setTimeout(r, 400));
  const bright = await page.$$eval('[data-testid="verdict-map"] [data-invariant]', (cs) => cs.filter((c) => getComputedStyle(c).opacity === "1").map((c) => c.getAttribute("data-invariant")).sort());
  const differing = Object.keys(statusesOf(run.oracle)).filter((id) => new Set([statusesOf(run.pre_repair)[id], statusesOf(run.oracle)[id], statusesOf(run.unsafe)[id]]).size > 1).sort();
  assert.deepEqual(bright, differing, "only the cells where candidates differ stay lit");
  // Code view shows the unsafe candidate's own patch.
  await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Code").click());
  if (run.unsafe.diff) {
    await page.waitForSelector('[data-testid="demo-unsafe-diff"]');
    const unsafeDiff = await text(page, '[data-testid="demo-unsafe-diff"]');
    assert.ok(unsafeDiff.includes("Seeded unsafe candidate"), "the unsafe candidate's own code is shown");
    assert.ok(!unsafeDiff.includes("'FULFILL'"), "the unsafe diff is not the repair");
    await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Compare both patches").click());
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="demo-diff"], [data-testid="demo-unsafe-diff"]').length === 2);
  }

  // EVIDENCE: the time map's areas follow the recorded durations.
  await go(page, "evidence");
  await page.waitForSelector('[data-testid="time-map"] [data-step]');
  const areas = await page.$$eval('[data-testid="time-map"] [data-step] > path:first-of-type', (ps) => ps.map((p) => ({ n: p.parentElement.getAttribute("data-step"), a: p.getBBox().width * p.getBBox().height })));
  assert.equal(areas.length, run.verification.steps.length, "one cell per sandbox step");
  const longest = [...run.verification.steps].sort((a, b) => b.duration_ms - a.duration_ms)[0].name;
  assert.equal([...areas].sort((a, b) => b.a - a.a)[0].n, longest, "the longest step has the largest cell");
  await page.evaluate(() => document.querySelectorAll('[data-testid="observation-row"]')[document.querySelectorAll('[data-testid="observation-row"]').length - 1].click());
  await page.waitForFunction((id) => document.querySelector('[data-testid="call-detail"]')?.innerText.includes(id), {}, capture.capture_id);
  assert.ok((await page.evaluate(() => document.body.innerText)).toLowerCase().includes("verification order"), "provider calls are tied to the verification order");

  // The human gate is pinned in every view and expands.
  for (const view of ["break", "fix", "judge", "evidence"]) {
    await go(page, view);
    const box = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(box && box.y + box.height > 940, `human gate is visible at the foot of ${view}`);
  }
  await page.evaluate(() => document.querySelector('[data-testid="human-gate"] button[aria-expanded]').click());
  await page.waitForSelector('[data-testid="demo-human"]');
  const human = await text(page, '[data-testid="demo-human"]');
  assert.ok(human.includes("No approval has been recorded"), "approval state is explicit");
  assert.ok(/cannot merge, push, deploy, capture, refund or retry/.test(human), "human-review boundary copy is visible");
  const overflow = await page.evaluate(() => ({ s: document.documentElement.scrollWidth, c: document.documentElement.clientWidth }));
  assert.ok(overflow.s <= overflow.c + 1, "desktop page has no horizontal overflow");

  // Phones: no overflow, readable type, a visible map, 44px nav targets, a pinned gate.
  for (const width of [390, 375]) {
    await page.setViewport({ width, height: 844, deviceScaleFactor: 1, isMobile: true, hasTouch: true });
    await page.goto(`${base}${pagePath}`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector('[data-testid="verdict-map"] [data-invariant]');
    const m = await page.evaluate(() => ({
      s: document.documentElement.scrollWidth,
      c: document.documentElement.clientWidth,
      svg: document.querySelector('[data-testid="verdict-map"] svg').getBoundingClientRect().width,
      tiny: [...document.querySelectorAll("body *")].filter((e) => !e.closest("svg") && [...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim()) && parseFloat(getComputedStyle(e).fontSize) < 12.5).map((e) => e.textContent.trim().slice(0, 30)),
      tinySvg: [...document.querySelectorAll("svg text")].filter((t) => parseFloat(getComputedStyle(t).fontSize) < 12.5).length,
    }));
    assert.ok(m.s <= m.c + 1, `${width}px page has no horizontal overflow`);
    assert.ok(m.svg >= width - 40, `${width}px map fills the width`);
    assert.deepEqual(m.tiny, [], `${width}px has no text under 12.5px`);
    assert.equal(m.tinySvg, 0, `${width}px map labels are readable`);
    const navBox = await (await page.$('nav[aria-label="Views"] a[href="#judge"]')).boundingBox();
    assert.ok(navBox.height >= 44, "view navigation has a 44px touch target");
    const gate = await (await page.$('[data-testid="human-gate"]')).boundingBox();
    assert.ok(gate.y + gate.height >= 830, "the gate is pinned on phones");
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
  assert.ok((await home.evaluate(() => document.body.innerText)).toLowerCase().includes("duplicate fulfillment"), "public entry loads the recorded case");

  assert.equal(pageErrors.length, 0, `page errors: ${pageErrors.join("; ")}`);
  console.log(`demo-browser: PASS run=${run.run.run_id}`);
} finally {
  await browser.close();
}
