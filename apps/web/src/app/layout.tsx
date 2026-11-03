import type { Metadata } from "next";
import Link from "next/link";
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";

import { TopNav } from "@/components/top-nav";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Waypoint - Verified Payment Repair",
    template: "%s - Waypoint",
  },
  description:
    "An AI engineer investigates broken PayPal integrations, proposes source repairs, and an independent financial oracle verifies the resulting monetary behavior before human review.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable}`}>
      <body className="min-h-dvh bg-background text-ink antialiased">
        <div className="mx-auto flex min-h-dvh w-full max-w-[1440px] flex-col px-5 sm:px-8 lg:px-12">
          <header className="flex h-16 shrink-0 items-center justify-between border-b border-line">
            <div className="flex min-w-0 items-center gap-6 sm:gap-10">
              <Link href="/" className="group flex shrink-0 items-baseline gap-2">
                <span className="text-[15px] font-semibold tracking-[-0.03em] text-ink">
                  Waypoint
                </span>
                <span className="hidden font-mono text-[9px] uppercase tracking-[0.14em] text-faint sm:inline">
                  payment repair
                </span>
              </Link>
              <TopNav />
            </div>
            <p className="hidden font-mono text-[10px] uppercase tracking-[0.12em] text-faint lg:block">
              Tier F evidence / no live payments
            </p>
          </header>
          <main className="flex-1 py-8 sm:py-10">{children}</main>
          <footer className="flex flex-col justify-between gap-4 border-t border-line py-7 text-[11px] leading-5 text-muted sm:flex-row sm:items-start">
            <p className="max-w-[78ch]">
              Waypoint records evidence. It does not move money. Provider interactions are
              deterministic tier-F fixtures; no PayPal Sandbox transaction or signed webhook
              was used. A repair reaches human review only as a candidate.
            </p>
            <p className="shrink-0 font-mono text-[10px] text-faint">
              RECORDED / REPLAY / NO LIVE CAPTURES
            </p>
          </footer>
        </div>
      </body>
    </html>
  );
}
