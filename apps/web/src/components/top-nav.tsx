"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "Incidents" },
  { href: "/benchmark", label: "Benchmark" },
] as const;

export function TopNav() {
  const pathname = usePathname();

  return (
    <nav aria-label="Primary" className="flex items-center gap-5">
      {links.map((link) => {
        const active =
          link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
        return (
          <Link
            key={link.href}
            href={link.href}
            aria-current={active ? "page" : undefined}
            className={`pb-0.5 text-sm ${
              active
                ? "border-b border-ink font-medium text-ink"
                : "text-muted transition-colors hover:text-ink"
            }`}
          >
            {link.label}
          </Link>
        );
      })}
    </nav>
  );
}
