import { Showcase } from "@/components/showcase";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: "Local repair replay",
  description:
    "The local Waypoint repair replay, using the same sanitized evidence projection as the public recorded case file.",
};

export default async function DemoPage() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="demo" />;
}
