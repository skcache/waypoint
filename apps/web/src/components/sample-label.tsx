import type { ReactNode } from "react";

/**
 * Marks synthetic content that must not be mistaken for live or
 * provider-confirmed data.
 */
export function SampleLabel({ children = "Sample" }: { children?: ReactNode }) {
  return (
    <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-faint">
      {children}
    </span>
  );
}
