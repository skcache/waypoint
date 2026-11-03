"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "Case file", short: "Case" },
  { href: "/benchmark", label: "Benchmark", short: "Benchmark" },
  { href: "/demo", label: "Local replay", short: "Replay" },
] as const;

export function TopNav() {
  const pathname = usePathname();

  return (
    <nav aria-label="Primary" className="flex items-center gap-4 sm:gap-6">
      {links.map((link) => {
        const active =
          link.href === "/"
            ? pathname === "/" || pathname.startsWith("/incidents")
            : pathname === link.href || pathname.startsWith(`${link.href}/`);
        return (
          <Link
            key={link.href}
            href={link.href}
            aria-current={active ? "page" : undefined}
            className={`relative whitespace-nowrap py-5 text-[12px] transition-colors ${
              active
                ? "text-ink after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-accent"
                : "text-muted hover:text-ink"
            }`}
          >
            <span className="sm:hidden">{link.short}</span><span className="hidden sm:inline">{link.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
