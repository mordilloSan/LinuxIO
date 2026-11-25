import { createFileRoute, redirect } from "@tanstack/react-router";

import { optionalString } from "@/routes/-search";

import { validateBtrfsView } from "./-components/btrfs/btrfsViews";

// Subvolumes became a view of the Snapshots tab; alerts and saved runs still
// link here, with the view and the pool they meant.
export const Route = createFileRoute("/_authenticated/storage/subvolumes")({
  validateSearch: (search) => ({
    ...optionalString(search, "fs"),
    ...validateBtrfsView(search),
  }),
  beforeLoad: ({ search }) => {
    throw redirect({
      replace: true,
      search: { fs: search.fs, view: search.view ?? "subvolumes" },
      to: "/storage/snapshots",
    });
  },
  // Never shown: beforeLoad always redirects. A component keeps the route
  // file in its own chunk like every other.
  component: () => null,
});
