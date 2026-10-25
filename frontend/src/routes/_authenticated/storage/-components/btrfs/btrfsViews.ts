import type { SearchInput } from "@/routes/-search";

// The Subvolumes tab's views and the ?view= values they put in the URL, so
// the selector and the route's validator cannot drift apart. Kept apart from
// the page so the route can validate eagerly without pulling the page out of
// its lazy chunk.
export const BTRFS_VIEWS = [
  { label: "Subvolumes", value: "subvolumes" },
  { label: "Snapshots", value: "snapshots" },
  { label: "Replication", value: "replication" },
] as const;

export type BtrfsView = (typeof BTRFS_VIEWS)[number]["value"];

export const DEFAULT_BTRFS_VIEW: BtrfsView = "subvolumes";

/** An unknown view is dropped, so the page falls back to Subvolumes. */
export const validateBtrfsView = (search: SearchInput): { view?: BtrfsView } =>
  BTRFS_VIEWS.some((view) => view.value === search.view)
    ? { view: search.view as BtrfsView }
    : {};
