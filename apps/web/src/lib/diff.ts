// Unified-diff parsing and a small Go/SQL line tokenizer for the code viewer.
export type DiffKind = "meta" | "hunk" | "add" | "del" | "context";

export interface DiffLine {
  kind: DiffKind;
  text: string;
  oldNo?: number;
  newNo?: number;
  /** Index of the hunk this line belongs to, -1 for the file header. */
  hunk: number;
}

export interface Hunk {
  index: number;
  header: string;
  /** Function/context text git puts after the second @@. */
  context: string;
  added: number;
  removed: number;
}

export function parseDiff(text: string): { lines: DiffLine[]; hunks: Hunk[] } {
  const lines: DiffLine[] = [];
  const hunks: Hunk[] = [];
  let oldNo = 0;
  let newNo = 0;
  let hunk = -1;
  const raw = text.split("\n");
  if (raw[raw.length - 1] === "") raw.pop();
  for (const line of raw) {
    if (line.startsWith("@@")) {
      const match = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/);
      if (match) {
        oldNo = Number(match[1]);
        newNo = Number(match[2]);
      }
      hunk += 1;
      hunks.push({ index: hunk, header: line, context: match?.[3] ?? "", added: 0, removed: 0 });
      lines.push({ kind: "hunk", text: line, hunk });
      continue;
    }
    if (hunk < 0) {
      lines.push({ kind: "meta", text: line, hunk });
      continue;
    }
    if (line.startsWith("+")) {
      hunks[hunk].added += 1;
      lines.push({ kind: "add", text: line.slice(1), newNo, hunk });
      newNo += 1;
    } else if (line.startsWith("-")) {
      hunks[hunk].removed += 1;
      lines.push({ kind: "del", text: line.slice(1), oldNo, hunk });
      oldNo += 1;
    } else {
      lines.push({ kind: "context", text: line.startsWith(" ") ? line.slice(1) : line, oldNo, newNo, hunk });
      oldNo += 1;
      newNo += 1;
    }
  }
  return { lines, hunks };
}

export function diffStats(text: string): { added: number; removed: number } {
  const { hunks } = parseDiff(text);
  return {
    added: hunks.reduce((n, h) => n + h.added, 0),
    removed: hunks.reduce((n, h) => n + h.removed, 0),
  };
}

export type TokenKind = "keyword" | "string" | "comment" | "number" | "type" | "sql" | "plain";

const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for",
  "func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select",
  "struct", "switch", "type", "var", "nil", "true", "false",
]);
const SQL_WORDS = new Set([
  "SELECT", "INSERT", "INTO", "VALUES", "UPDATE", "SET", "WHERE", "ON", "CONFLICT", "DO", "NOTHING", "FROM", "AND",
]);

const TOKEN = /(\/\/.*$|`(?:[^`]*)(?:`|$)|"(?:\\.|[^"\\])*"|\b\d+(?:\.\d+)?\b|\b[A-Za-z_][A-Za-z0-9_]*\b)/g;

export function tokenizeLine(text: string): { kind: TokenKind; text: string }[] {
  const out: { kind: TokenKind; text: string }[] = [];
  let last = 0;
  let afterFunc = false;
  for (const m of text.matchAll(TOKEN)) {
    const start = m.index ?? 0;
    if (start > last) out.push({ kind: "plain", text: text.slice(last, start) });
    const tok = m[0];
    let kind: TokenKind = "plain";
    if (tok.startsWith("//")) kind = "comment";
    else if (tok.startsWith("`") || tok.startsWith('"')) kind = "string";
    else if (/^\d/.test(tok)) kind = "number";
    else if (GO_KEYWORDS.has(tok)) kind = "keyword";
    else if (SQL_WORDS.has(tok)) kind = "sql";
    else if (afterFunc || /^[A-Z][A-Za-z0-9]*$/.test(tok)) kind = "type";
    afterFunc = tok === "func";
    out.push({ kind, text: tok });
    last = start + tok.length;
  }
  if (last < text.length) out.push({ kind: "plain", text: text.slice(last) });
  return out;
}
