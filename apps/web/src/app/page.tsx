import { Showcase } from "@/components/waypoint/experience";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: { absolute: "Waypoint - An AI repaired the bug. A separate judge checked the ledger." },
  description:
    "A recorded Waypoint run: an AI diagnoses a payment-fulfillment bug and writes a source patch, then an independent financial oracle accepts the repair and rejects a seeded unsafe one.",
};

export default async function Page() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="showcase" />;
}
