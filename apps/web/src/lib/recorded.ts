import recordedJson from "../../public/demo/recorded.json";

import type { DemoExport } from "@/lib/demo";

/**
 * The committed, sanitized evidence projection is bundled at build time.
 * The public showcase never needs PostgreSQL, Docker, a model, or PayPal.
 *
 * `/demo` still prefers a fresh local export at runtime; this committed file is
 * the safe fallback and the public Vercel default.
 */
export async function loadRecordedDemo(): Promise<DemoExport | null> {
  return recordedJson as unknown as DemoExport;
}
