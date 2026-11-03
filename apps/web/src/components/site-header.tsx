"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

export function isCaseFile(pathname: string): boolean {
  return pathname === "/" || pathname === "/demo" || pathname === "/incidents";
}

/** Header for the review console pages. The case file renders its own nav inside the app shell. */
export function SiteHeader() {
  const pathname = usePathname();
  if (isCaseFile(pathname)) return null;
  const link = (on: boolean) =>
    `relative inline-flex h-12 items-center px-3 text-[15px] transition-colors ${on ? "text-ink underline decoration-line-strong underline-offset-[10px]" : "text-faint hover:text-ink"}`;
  return (
    <header className="border-b border-line bg-background">
      <div className="mx-auto flex h-14 w-full max-w-[1280px] items-center justify-between gap-3 px-3 sm:px-8 lg:px-12">
        <Link href="/" className="flex min-h-11 shrink-0 items-center px-2 font-semibold tracking-[-0.01em]" aria-label="Waypoint home">
          Waypoint
        </Link>
        <nav aria-label="Primary" className="flex items-center">
          <Link href="/" className={link(false)}>Case file</Link>
          <Link href="/benchmark" className={link(pathname.startsWith("/benchmark"))}>Benchmark</Link>
        </nav>
      </div>
    </header>
  );
}
