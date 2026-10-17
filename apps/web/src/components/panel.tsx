import type { ReactNode } from "react";

/**
 * Bordered panel with a micro header. One shared container so the pages keep
 * a single border and radius system.
 */
export function Panel({
  title,
  label,
  className,
  children,
}: {
  title: string;
  label?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      className={`rounded-panel border border-line bg-surface ${className ?? ""}`}
    >
      <header className="flex h-10 items-center justify-between gap-3 border-b border-line px-4">
        <h2 className="text-xs font-medium uppercase tracking-wider text-muted">
          {title}
        </h2>
        {label}
      </header>
      <div className="px-4 py-4">{children}</div>
    </section>
  );
}
