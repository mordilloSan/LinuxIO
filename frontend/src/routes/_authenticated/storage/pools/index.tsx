import { createFileRoute } from "@tanstack/react-router";

// With no pool selected the parent layout shows the list instead of the
// outlet, so this index route renders nothing of its own.
export const Route = createFileRoute("/_authenticated/storage/pools/")({
  component: () => null,
});
