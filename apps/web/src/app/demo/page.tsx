import { Showcase } from "@/components/waypoint/experience";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: { absolute: "Waypoint - AI payment-bug repair, verified by an independent checker" },
  description:
    "A recorded Waypoint run: an AI diagnoses a duplicate-fulfillment bug in a PayPal integration and writes a patch, then a separate checker tests it against 14 money rules, accepts the repair and rejects a seeded unsafe patch.",
};

export default async function DemoPage() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="demo" />;
}
