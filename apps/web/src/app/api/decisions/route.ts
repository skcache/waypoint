import { NextResponse } from "next/server";
import { appendFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { isAuthorized, reviewEnabled } from "@/lib/review-guard.mjs";


const decisions = new Set(["accept_for_integration", "request_changes", "reject"]);

// The console is public. Recording a human decision is disabled unless the
// deployment explicitly configures WAYPOINT_REVIEW_TOKEN, and even then it
// requires that exact bearer token. Nothing here can merge, deploy, capture,
// refund, or retry a payment.
export async function GET() {
  return NextResponse.json({ enabled: reviewEnabled() });
}

export async function POST(request: Request) {
  if (!reviewEnabled()) {
    return NextResponse.json(
      { error: "review recording is disabled in this deployment" },
      { status: 403 },
    );
  }
  if (!isAuthorized(request.headers)) {
    return NextResponse.json({ error: "unauthorized reviewer" }, { status: 401 });
  }

  let body: { case_id?: string; condition?: string; decision?: string; note?: string };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON" }, { status: 400 });
  }
  if (!body.case_id || !body.condition || !body.decision || !decisions.has(body.decision)) {
    return NextResponse.json(
      { error: "case_id, condition and a valid decision are required" },
      { status: 400 },
    );
  }
  const record = {
    case_id: String(body.case_id).slice(0, 120),
    condition: String(body.condition).slice(0, 20),
    decision: body.decision,
    note: typeof body.note === "string" ? body.note.slice(0, 2000) : "",
    recorded_at: new Date().toISOString(),
  };
  const dir = process.cwd();
  await mkdir(dir, { recursive: true });
  await appendFile(path.join(dir, ".decisions.jsonl"), JSON.stringify(record) + "\n", "utf8");
  return NextResponse.json({ ok: true, recorded_at: record.recorded_at });
}
