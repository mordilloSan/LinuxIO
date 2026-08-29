import { describe, expect, it } from "vitest";

import type { DirectoryListing } from "@/api";
import {
  ensureTarGzExtension,
  ensureZipExtension,
  getTextEditBlockedReason,
  isArchiveFile,
  normalizeResource,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";

describe("filebrowser utils", () => {
  it("normalizes directory resources into a combined item list with stable paths", () => {
    const resource = normalizeResource(
      {
        files: [
          {
            canOpenAsText: true,
            isRegularFile: true,
            modified: "2026-01-01T00:00:00Z",
            name: "compose.yaml",
            size: 1,
            symlink: false,
          },
        ],
        folders: [
          {
            modified: "2026-01-01T00:00:00Z",
            name: "stacks",
            symlink: false,
          },
        ],
      } satisfies DirectoryListing,
      "/srv/docker",
    );
    expect(resource.items).toEqual([
      expect.objectContaining({
        name: "stacks",
        path: "/srv/docker/stacks/",
      }),
      expect.objectContaining({
        modTime: "2026-01-01T00:00:00Z",
        name: "compose.yaml",
        path: "/srv/docker/compose.yaml",
      }),
    ]);
  });

  it("normalizes root directory children without duplicate slashes", () => {
    const resource = normalizeResource(
      {
        files: [
          {
            canOpenAsText: true,
            isRegularFile: true,
            modified: "2026-01-01T00:00:00Z",
            name: "motd",
            size: 1,
            symlink: false,
          },
        ],
        folders: [
          {
            modified: "2026-01-01T00:00:00Z",
            name: "etc",
            symlink: false,
          },
        ],
      } satisfies DirectoryListing,
      "/",
    );

    expect(resource.items?.map((item) => item.path)).toEqual([
      "/etc/",
      "/motd",
    ]);
  });

  it("detects and strips supported archive extensions case-insensitively", () => {
    expect(isArchiveFile("backup.ZIP")).toBe(true);
    expect(isArchiveFile("backup.tar.gz")).toBe(true);
    expect(isArchiveFile("backup.tgz")).toBe(true);
    expect(isArchiveFile("backup.tar.xz")).toBe(false);
    expect(stripArchiveExtension("backup.tar.gz")).toBe("backup");
    expect(stripArchiveExtension("backup.tgz")).toBe("backup");
    expect(stripArchiveExtension("backup.zip")).toBe("backup");
    expect(stripArchiveExtension("backup.txt")).toBe("backup.txt");
  });

  it("adds archive extensions only when missing", () => {
    expect(ensureZipExtension("backup")).toBe("backup.zip");
    expect(ensureZipExtension("backup.ZIP")).toBe("backup.ZIP");
    expect(ensureTarGzExtension("backup")).toBe("backup.tar.gz");
    expect(ensureTarGzExtension("backup.TAR.GZ")).toBe("backup.TAR.GZ");
  });

  it("explains why an entry cannot be opened in the text editor", () => {
    expect(
      getTextEditBlockedReason({ canOpenAsText: true, isRegularFile: true }),
    ).toBeNull();
    expect(
      getTextEditBlockedReason({ isRegularFile: true, size: 1_000_000 }),
    ).toMatch(/1 MB or larger/);
    expect(
      getTextEditBlockedReason({ isRegularFile: true, size: 999_999 }),
    ).toMatch(/isn't plain text/);
    expect(getTextEditBlockedReason({ isRegularFile: false })).toMatch(
      /Only regular files/,
    );
  });
});
