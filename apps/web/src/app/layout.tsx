import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Waypoint — AI-aware transaction execution boundary",
  description:
    "An intent-aware execution boundary for AI-initiated financial transactions. Human-confirmed mandates, deterministic policy, PayPal sandbox execution, and correlated traces. Early prototype — database foundation in place; integrations not configured.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body className="bg-white text-zinc-900 antialiased">{children}</body>
    </html>
  );
}
