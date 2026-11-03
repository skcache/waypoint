import { Showcase } from "@/components/showcase";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: "Verified payment repair",
  description:
    "An AI engineer investigates a failed PayPal integration, proposes a source repair, and an independent oracle verifies the financial behavior before human review.",
};

export default async function Page() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="showcase" />;
}
