import { timingSafeEqual } from "node:crypto";

const MIN_TOKEN_LENGTH = 16;

/**
 * Review recording is disabled unless the deployment explicitly configures a
 * reviewer token. There is no default credential and no anonymous mode.
 */
export function reviewEnabled(env = process.env) {
  return typeof env.WAYPOINT_REVIEW_TOKEN === "string" && env.WAYPOINT_REVIEW_TOKEN.length >= MIN_TOKEN_LENGTH;
}

/**
 * Accepts only an exact `Authorization: Bearer <token>` match. The token is
 * never logged, echoed or placed in the client bundle.
 */
export function isAuthorized(headers, env = process.env) {
  const token = env.WAYPOINT_REVIEW_TOKEN;
  if (typeof token !== "string" || token.length < MIN_TOKEN_LENGTH) return false;
  const raw = headers?.get?.("authorization") ?? headers?.authorization ?? "";
  if (typeof raw !== "string" || !raw.startsWith("Bearer ")) return false;
  const got = Buffer.from(raw.slice("Bearer ".length));
  const want = Buffer.from(token);
  return got.length === want.length && timingSafeEqual(got, want);
}
