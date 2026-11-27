import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { linuxio } from "@/api";

import { knownDiskSize } from "./knownDiskSize";

const listed = (client: QueryClient) => {
  client.setQueryData(
    linuxio.filebrowser.list_directory({ path: "/srv" }).queryKey,
    {
      files: [{ name: "notes.txt", size: 10 }],
      folders: [{ name: "go-build" }, { name: "new" }],
    },
  );
  client.setQueryData(
    linuxio.filebrowser.subfolders({ path: "/srv" }).queryKey,
    { pending: false, subfolders: [{ path: "/srv/go-build", size: 4096 }] },
  );
  return client;
};

describe("knownDiskSize", () => {
  it("adds a file's listed size to a folder's size in the index", () => {
    const client = listed(new QueryClient());
    expect(knownDiskSize(client, ["/srv/go-build/", "/srv/notes.txt"])).toBe(
      4106,
    );
  });

  it("is unknown when the index holds no size for one of the folders", () => {
    const client = listed(new QueryClient());
    expect(knownDiskSize(client, ["/srv/notes.txt", "/srv/new/"])).toBe(
      undefined,
    );
  });

  it("is unknown while the index is still catching up with the folder", async () => {
    const pending = listed(new QueryClient());
    pending.setQueryData(
      linuxio.filebrowser.subfolders({ path: "/srv" }).queryKey,
      { pending: true, subfolders: [{ path: "/srv/go-build", size: 0 }] },
    );
    expect(knownDiskSize(pending, ["/srv/go-build/"])).toBe(undefined);

    // Marked stale by a listing that has just arrived: the size is old.
    const stale = listed(new QueryClient());
    await stale.invalidateQueries({
      queryKey: linuxio.filebrowser.subfolders({ path: "/srv" }).queryKey,
    });
    expect(knownDiskSize(stale, ["/srv/go-build/"])).toBe(undefined);
    // A file's size is the listing's own and does not wait for the index.
    expect(knownDiskSize(stale, ["/srv/notes.txt"])).toBe(10);
  });

  it("is unknown for an item of a folder the page holds no listing of", () => {
    const client = listed(new QueryClient());
    expect(knownDiskSize(client, ["/etc/hosts"])).toBe(undefined);
    expect(knownDiskSize(new QueryClient(), ["/srv/notes.txt"])).toBe(
      undefined,
    );
  });
});
