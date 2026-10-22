import { NextResponse } from "next/server";
import { appendFile, mkdir } from "node:fs/promises";
import path from "node:path";

const decisions = new Set(["accept_for_integration", "request_changes", "reject"]);

export async function POST(request: Request) {
  let body: { case_id?: string; condition?: string; decision?: string; note?: string };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON" }, { status: 400 });
  }
  if (!body.case_id || !body.condition || !body.decision || !decisions.has(body.decision)) {
    return NextResponse.json({ error: "case_id, condition and a valid decision are required" }, { status: 400 });
  }
  const record = {
    case_id: String(body.case_id).slice(0, 120),
    condition: String(body.condition).slice(0, 20),
    decision: body.decision,
    note: typeof body.note === "string" ? body.note.slice(0, 2000) : "",
    recorded_at: new Date().toISOString(),
  };
  // A local review record only. This endpoint never touches payment state, code,
  // git, a model, or a deployment.
  const dir = process.cwd();
  await mkdir(dir, { recursive: true });
  await appendFile(path.join(dir, ".decisions.jsonl"), JSON.stringify(record) + "\n", "utf8");
  return NextResponse.json({ ok: true, recorded_at: record.recorded_at });
}
