import Page from "../page";

// The canonical list is rendered at both / and /incidents. The documented
// workbench entry point is /incidents; keeping the alias avoids a dead link
// from the README and the demo script.
export const metadata = { title: "Incidents" };

export default Page;
