import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render } from "@/test/render";

import PoolFolderFields, {
  poolFolderError,
  sharePools,
  type PoolFolder,
} from "./PoolFolderFields";

const fs = (
  uuid: string,
  label: string,
  mounts: [string, number][],
): BtrfsFilesystem =>
  ({
    uuid,
    label,
    mountpoints: mounts.map(([mountpoint, subvolId]) => ({
      mountpoint,
      subvolId,
    })),
  }) as BtrfsFilesystem;

const folder: PoolFolder = {
  uuid: "u1",
  name: "media",
  limitGiB: "",
  snapshots: true,
};

describe("sharePools", () => {
  it("offers pools with a permanent top-level mount only", () => {
    expect(
      sharePools([
        fs("u1", "tank", [["/mnt/tank", 5]]),
        fs("u2", "", [["/", 256]]),
        fs("u3", "cold", [["/run/linuxio/btrfs/u3", 5]]),
        fs("u4", "", [
          ["/home", 257],
          ["/srv/pool", 5],
        ]),
      ]),
    ).toEqual([
      { uuid: "u1", name: "tank", mountpoint: "/mnt/tank" },
      { uuid: "u4", name: "u4", mountpoint: "/srv/pool" },
    ]);
  });
});

describe("poolFolderError", () => {
  it("accepts a plain name and an optional positive limit", () => {
    expect(poolFolderError(folder)).toBeNull();
    expect(poolFolderError({ ...folder, limitGiB: "1.5" })).toBeNull();
  });

  it("rejects what would break the share path or the snapshot area", () => {
    for (const bad of [
      { ...folder, uuid: "" },
      { ...folder, name: "" },
      { ...folder, name: "my media" },
      { ...folder, name: "a/b" },
      { ...folder, name: ".." },
      { ...folder, name: "linuxio-snapshots" },
      { ...folder, limitGiB: "0" },
      { ...folder, limitGiB: "-3" },
      { ...folder, limitGiB: "lots" },
    ]) {
      expect(poolFolderError(bad)).not.toBeNull();
    }
  });
});

describe("PoolFolderFields", () => {
  it("shows where the folder will be created and reports edits", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <PoolFolderFields
        folder={folder}
        onChange={onChange}
        pools={[{ uuid: "u1", name: "tank", mountpoint: "/mnt/tank" }]}
      />,
    );

    expect(screen.getByText("Created as /mnt/tank/media")).toBeInTheDocument();
    await user.click(
      screen.getByRole("checkbox", { name: /Automatic snapshots/ }),
    );
    expect(onChange).toHaveBeenCalledWith({ ...folder, snapshots: false });
  });
});
