"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "Workbench" },
  { href: "/checkout", label: "Checkout" },
  { href: "/trace", label: "Trace" },
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
            className={`text-sm ${
              active
                ? "font-medium text-ink"
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
