import { isRedirect } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { Route as IndexRoute } from "../index";
import { Route as SubvolumesRoute } from "../subvolumes";

type BeforeLoad = (args: {
  search: Record<string, string | undefined>;
  context: { access: { privileged: boolean; btrfsAvailable?: boolean } };
}) => void;

const admin = { access: { privileged: true, btrfsAvailable: true } };

const redirectOf = (beforeLoad: BeforeLoad, search: Record<string, string>) => {
  try {
    beforeLoad({ search, context: admin });
  } catch (error) {
    expect(isRedirect(error)).toBe(true);
    return (error as { options: unknown }).options;
  }
  return null;
};

describe("old storage links", () => {
  it("send a drive, volume or array selection to the Disks page", () => {
    const beforeLoad = IndexRoute.options.beforeLoad as BeforeLoad;
    expect(redirectOf(beforeLoad, { drive: "sda" })).toMatchObject({
      to: "/storage/disks",
      search: { drive: "sda" },
    });
    expect(redirectOf(beforeLoad, { raid: "md127" })).toMatchObject({
      to: "/storage/disks",
      search: { raid: "md127" },
    });
    expect(redirectOf(beforeLoad, {})).toBeNull();
  });

  it("send Subvolumes to Snapshots, keeping the view and the pool", () => {
    const beforeLoad = SubvolumesRoute.options.beforeLoad as BeforeLoad;
    expect(
      redirectOf(beforeLoad, { view: "replication", fs: "uuid-1" }),
    ).toMatchObject({
      to: "/storage/snapshots",
      search: { view: "replication", fs: "uuid-1" },
    });
    expect(redirectOf(beforeLoad, {})).toMatchObject({
      to: "/storage/snapshots",
      search: { view: "subvolumes" },
    });
  });
});
