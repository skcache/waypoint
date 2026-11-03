import { Showcase } from "@/components/waypoint/experience";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: { absolute: "Waypoint - AI repair of payment bugs with an independent check" },
  description:
    "A recorded Waypoint run. An AI finds a duplicate fulfillment bug in a PayPal integration and writes a patch. A separate checker tests the patch against 14 rules. It accepts the repair and rejects an unsafe patch.",
};

export default async function Page() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="showcase" />;
}
