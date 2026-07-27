import { createFileRoute } from "@tanstack/react-router";

import VMNetworksPage from "./-components/VMNetworksPage";

export const Route = createFileRoute("/_authenticated/vm/networks")({
  component: VMNetworksPage,
});
