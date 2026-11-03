import type { Metadata } from "next";
import { GeistMono } from "geist/font/mono";

import "@fontsource-variable/archivo/wdth.css";
import { SiteHeader } from "@/components/site-header";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Waypoint - An AI repaired the bug. A separate judge checked the ledger.",
    template: "%s - Waypoint",
  },
  description:
    "A recorded Waypoint run: an AI diagnoses a payment-fulfillment bug and writes a source patch, then an independent financial oracle accepts the repair and rejects a seeded unsafe one.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={GeistMono.variable}>
      <body className="min-h-dvh antialiased">
        <SiteHeader />
        {children}
      </body>
    </html>
  );
}
