"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

const ACTS = [
  { id: "break", label: "Break" },
  { id: "fix", label: "Fix" },
  { id: "judge", label: "Judge" },
] as const;

export function isCaseFile(pathname: string): boolean {
  return pathname === "/" || pathname === "/demo" || pathname === "/incidents";
}

export function SiteHeader() {
  const pathname = usePathname();
  const caseFile = isCaseFile(pathname);
  const [active, setActive] = useState<string>("top");

  useEffect(() => {
    if (!caseFile) return;
    const ids = ["top", ...ACTS.map((a) => a.id)];
    const targets = ids.map((id) => document.getElementById(id)).filter(Boolean) as HTMLElement[];
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) if (entry.isIntersecting) setActive(entry.target.id);
      },
      { rootMargin: "-35% 0px -60% 0px" },
    );
    targets.forEach((t) => observer.observe(t));
    return () => observer.disconnect();
  }, [caseFile]);

  const link = (on: boolean) =>
    `relative inline-flex h-12 items-center px-3 text-[15px] transition-colors ${on ? "font-medium text-ink" : "text-muted hover:text-ink"}`;
  const mark = (on: boolean) => (on ? <span className="absolute inset-x-3 bottom-0 h-0.5 bg-accent" /> : null);
  const dark = caseFile && active === "judge";

  return (
    <header className={`sticky top-0 z-40 border-b border-line bg-background transition-colors duration-200 ${dark ? "night" : ""}`}>
      <div className="mx-auto flex h-14 w-full max-w-[1280px] items-center justify-between gap-3 px-3 sm:px-8 lg:px-12">
        <Link href="/" className="flex min-h-11 shrink-0 items-center px-2 text-lg font-semibold tracking-[-0.02em]" aria-label="Waypoint home">
          Waypoint
        </Link>
        {caseFile ? (
          <nav aria-label="Acts" className="flex items-center">
            {ACTS.map((act) => (
              <a key={act.id} href={`#${act.id}`} aria-current={active === act.id ? "location" : undefined} className={link(active === act.id)}>
                {act.label}
                {mark(active === act.id)}
              </a>
            ))}
            <Link href="/benchmark" className={`${link(false)} max-sm:hidden`}>Benchmark</Link>
          </nav>
        ) : (
          <nav aria-label="Primary" className="flex items-center">
            <Link href="/" className={link(false)}>Case file</Link>
            <Link href="/benchmark" className={link(pathname.startsWith("/benchmark"))}>
              Benchmark
              {mark(pathname.startsWith("/benchmark"))}
            </Link>
          </nav>
        )}
      </div>
    </header>
  );
}
