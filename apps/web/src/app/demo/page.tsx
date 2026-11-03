import { Showcase } from "@/components/showcase";
import { loadRecordedDemo } from "@/lib/recorded";

export const metadata = {
  title: "Local repair run",
  description:
    "A local Waypoint repair run, using the same sanitized evidence projection as the public recorded case file. Falls back to committed evidence when no local export exists.",
};

export default async function DemoPage() {
  const initial = await loadRecordedDemo();
  return <Showcase initial={initial} variant="demo" />;
}
