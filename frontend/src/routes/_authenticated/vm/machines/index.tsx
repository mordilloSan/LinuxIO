import { createFileRoute } from "@tanstack/react-router";

import VMDetailsPanel from "../-components/VMDetailsPanel";

export const Route = createFileRoute("/_authenticated/vm/machines/")({
  component: VMMachinesEmptyDetail,
});

function VMMachinesEmptyDetail() {
  return <VMDetailsPanel vm={null} />;
}
