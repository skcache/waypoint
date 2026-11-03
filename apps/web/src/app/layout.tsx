import type { Metadata } from "next";
import { GeistMono } from "geist/font/mono";

import "@fontsource-variable/archivo/wdth.css";
import { SiteHeader } from "@/components/site-header";
import { themeScript } from "@/components/theme-toggle";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Waypoint - AI repair of payment bugs with an independent check",
    template: "%s - Waypoint",
  },
  description:
    "A recorded Waypoint run. An AI finds a duplicate fulfillment bug in a PayPal integration and writes a patch. A separate checker tests the patch against 14 rules. It accepts the repair and rejects an unsafe patch.",
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
