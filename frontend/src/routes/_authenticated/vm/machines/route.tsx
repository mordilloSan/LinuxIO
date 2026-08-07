import { createFileRoute } from "@tanstack/react-router";

import VMMachinesLayout from "../-components/VMMachinesLayout";

// The VM list and preflight status come from the `/vm` parent loader, so this
// layout owns no data of its own.
export const Route = createFileRoute("/_authenticated/vm/machines")({
  component: VMMachinesLayout,
});
