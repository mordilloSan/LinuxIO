import { createFileRoute } from "@tanstack/react-router";

import VMDashboardPage from "./-components/VMDashboardPage";

export const Route = createFileRoute("/_authenticated/vm/")({
  component: VMDashboardPage,
});
