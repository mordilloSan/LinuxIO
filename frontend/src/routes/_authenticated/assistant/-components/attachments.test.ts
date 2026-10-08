import { describe, expect, it, vi } from "vitest";

import {
  type Attachment,
  buildPrompt,
  fileAttachment,
  imageAttachment,
} from "./attachments";

describe("fileAttachment", () => {
  it("embeds a small text file as a resource", async () => {
    const attachment = await fileAttachment("/srv/a.txt", async () => "hi");
    expect(attachment).toEqual({
      id: expect.any(String),
      kind: "file",
      path: "/srv/a.txt",
      block: {
        type: "resource",
        resource: { uri: "file:///srv/a.txt", text: "hi" },
      },
    });
  });

  it("links a file over 200 KB", async () => {
    const attachment = await fileAttachment("/srv/big.log", async () =>
      "x".repeat(200_001),
    );
    expect(attachment.kind === "file" && attachment.block).toEqual({
      type: "resource_link",
      uri: "file:///srv/big.log",
      name: "big.log",
    });
  });

  it("links a file that cannot be read", async () => {
    const attachment = await fileAttachment("/srv/bin", async () => {
      throw new Error("binary");
    });
    expect(attachment.kind === "file" && attachment.block.type).toBe(
      "resource_link",
    );
  });
});

describe("attachment ids", () => {
  it("gives each attachment its own id, even for the same file", async () => {
    const [a, b] = await Promise.all([
      fileAttachment("/srv/a.txt", async () => "hi"),
      fileAttachment("/srv/a.txt", async () => "hi"),
    ]);
    expect(a.id).not.toBe(b.id);
  });

  it("falls back to a counter without crypto.randomUUID", async () => {
    vi.stubGlobal("crypto", {});
    try {
      const [a, b] = await Promise.all([
        fileAttachment("/srv/a.txt", async () => "hi"),
        fileAttachment("/srv/a.txt", async () => "hi"),
      ]);
      expect(a.id).not.toBe(b.id);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe("imageAttachment", () => {
  it("reads a file into base64 data with a preview URL", async () => {
    const file = new File(["abc"], "shot.png", { type: "image/png" });
    const attachment = await imageAttachment(file);
    expect(attachment).toMatchObject({
      kind: "image",
      name: "shot.png",
      mimeType: "image/png",
      data: "YWJj",
    });
  });
});

describe("imageAttachment size cap", () => {
  it("rejects an image over 5 MB and names the limit", async () => {
    const file = new File(["x"], "big.png", { type: "image/png" });
    Object.defineProperty(file, "size", { value: 5 * 1024 * 1024 + 1 });
    await expect(imageAttachment(file)).rejects.toThrow(/5 MB/);
  });
});

describe("buildPrompt", () => {
  it("orders images, then files, then the text", () => {
    const attachments: Attachment[] = [
      {
        id: "a",
        kind: "file",
        path: "/a",
        block: { type: "resource_link", uri: "file:///a", name: "a" },
      },
      {
        id: "i",
        kind: "image",
        name: "i.png",
        mimeType: "image/png",
        data: "AA==",
        previewUrl: "blob:x",
      },
    ];
    expect(buildPrompt("hello", attachments).map((b) => b.type)).toEqual([
      "image",
      "resource_link",
      "text",
    ]);
  });
});
