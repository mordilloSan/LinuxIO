import { describe, expect, it } from "vitest";

import type { ExtendedFileInfo } from "@/api";

import {
  ensureTarGzExtension,
  ensureZipExtension,
  isArchiveFile,
  isEditableFile,
  normalizeResource,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";

describe("filebrowser utils", () => {
  it("normalizes directory resources into a combined item list with stable paths", () => {
    const resource = normalizeResource({
      files: [
        {
          modified: "2026-01-01T00:00:00Z",
          name: "compose.yaml",
          type: "file",
        },
      ],
      folders: [
        {
          name: "stacks",
          type: "directory",
        },
      ],
      modified: "2026-01-02T00:00:00Z",
      name: "docker",
      path: "/srv/docker/",
      type: "directory",
    } as ExtendedFileInfo);

    expect(resource.modTime).toBe("2026-01-02T00:00:00Z");
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
    const resource = normalizeResource({
      files: [{ name: "motd", type: "file" }],
      folders: [{ name: "etc", type: "directory" }],
      name: "/",
      path: "/",
      type: "directory",
    } as ExtendedFileInfo);

    expect(resource.items?.map((item) => item.path)).toEqual([
      "/etc/",
      "/motd",
    ]);
  });

  it("returns non-directory resources unchanged", () => {
    const file = {
      content: "hello",
      name: "readme.md",
      path: "/readme.md",
      type: "file",
    } as ExtendedFileInfo;

    expect(normalizeResource(file)).toBe(file);
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

  it("identifies editable source, config, dotfile, and extensionless project files", () => {
    expect(isEditableFile("Dockerfile")).toBe(true);
    expect(isEditableFile(".bashrc")).toBe(true);
    expect(isEditableFile("compose.yaml")).toBe(true);
    expect(isEditableFile("main.go")).toBe(true);
    expect(isEditableFile("archive.zip")).toBe(false);
    expect(isEditableFile("binary")).toBe(false);
    expect(isEditableFile("trailing.")).toBe(false);
  });
});
