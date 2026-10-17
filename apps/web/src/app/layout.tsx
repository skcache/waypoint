import type { Metadata } from "next";
import Link from "next/link";
import { TopNav } from "@/components/top-nav";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Waypoint - Workbench preview",
    template: "%s - Waypoint",
  },
  description:
    "Static workbench preview of Waypoint: human-confirmed mandates, agent proposals, deterministic decisions, and PayPal sandbox checkout. Sample data only; no runtime is connected yet.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body className="bg-background text-ink antialiased">
        <div className="mx-auto flex min-h-dvh w-full max-w-6xl flex-col px-5 sm:px-8">
          <header className="flex h-14 items-center justify-between border-b border-line">
            <div className="flex items-center gap-7">
              <Link href="/" className="text-sm font-semibold tracking-tight">
                Waypoint
              </Link>
              <TopNav />
            </div>
            <p className="hidden text-xs text-muted md:block">
              Sandbox preview. No live payments.
            </p>
          </header>
          <main className="flex-1 py-10 sm:py-12">{children}</main>
          <footer className="border-t border-line py-6 text-xs leading-5 text-muted">
            <p>
              Static preview. Every value on these pages is sample data, and the
              runtime that produces real values arrives in later phases. PayPal
              sandbox only; no real money moves.
            </p>
          </footer>
        </div>
      </body>
    </html>
  );
}
