import type { Column } from "@/lib/evidence";

export type Candidate = Column;
export type EvidenceSource = "recorded" | "local";
export type View = "break" | "fix" | "judge" | "evidence";
export const VIEWS: { id: View; label: string }[] = [
  { id: "break", label: "Break" },
  { id: "fix", label: "Fix" },
  { id: "judge", label: "Judge" },
  { id: "evidence", label: "Evidence" },
];
