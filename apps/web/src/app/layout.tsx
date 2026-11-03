import type { Metadata } from "next";
import { GeistMono } from "geist/font/mono";

import "@fontsource-variable/archivo/wdth.css";
import { SiteHeader } from "@/components/site-header";
import { themeScript } from "@/components/theme-toggle";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Waypoint - AI payment-bug repair, verified by an independent checker",
    template: "%s - Waypoint",
  },
  description:
    "A recorded Waypoint run: an AI diagnoses a duplicate-fulfillment bug in a PayPal integration and writes a patch, then a separate checker tests it against 14 money rules, accepts the repair and rejects a seeded unsafe patch.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" data-theme="dark" className={GeistMono.variable} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="min-h-dvh antialiased">
        <SiteHeader />
        {children}
      </body>
    </html>
  );
}
