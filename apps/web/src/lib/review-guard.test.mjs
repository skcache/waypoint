import test from "node:test";
import assert from "node:assert/strict";
import { isAuthorized, reviewEnabled } from "./review-guard.mjs";

const h = (value) => new Headers(value ? { authorization: value } : {});
const TOKEN = "reviewer-token-0123456789";

test("recording is disabled by default and with a short token", () => {
  assert.equal(reviewEnabled({}), false);
  assert.equal(reviewEnabled({ WAYPOINT_REVIEW_TOKEN: "short" }), false);
  assert.equal(isAuthorized(h("Bearer " + TOKEN), {}), false);
});

test("an unauthenticated request is never authorized", () => {
  const env = { WAYPOINT_REVIEW_TOKEN: TOKEN };
  assert.equal(isAuthorized(h(undefined), env), false);
  assert.equal(isAuthorized(h("Bearer"), env), false);
  assert.equal(isAuthorized(h("Basic " + TOKEN), env), false);
  assert.equal(isAuthorized(h("Bearer wrong-token-value-123"), env), false);
  assert.equal(isAuthorized(h("Bearer " + TOKEN + "x"), env), false);
});

test("the exact configured bearer token is authorized", () => {
  const env = { WAYPOINT_REVIEW_TOKEN: TOKEN };
  assert.equal(reviewEnabled(env), true);
  assert.equal(isAuthorized(h("Bearer " + TOKEN), env), true);
});
