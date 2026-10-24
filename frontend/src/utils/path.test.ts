import { describe, expect, it } from "vitest";

import { baseName, joinPath, parentPath } from "@/utils/path";

describe("path helpers", () => {
  it("joins with exactly one slash", () => {
    expect(joinPath("/home/user", "file.txt")).toBe("/home/user/file.txt");
    expect(joinPath("/home/user/", "file.txt")).toBe("/home/user/file.txt");
  });

  it("returns the last segment", () => {
    expect(baseName("/home/user/file.txt")).toBe("file.txt");
    expect(baseName("/home/user/")).toBe("user");
    expect(baseName("/")).toBe("");
  });

  it("returns the parent directory", () => {
    expect(parentPath("/home/user/file.txt")).toBe("/home/user");
    expect(parentPath("/home/user/")).toBe("/home");
    expect(parentPath("/home")).toBe("/");
    expect(parentPath("/")).toBe("/");
  });
});
