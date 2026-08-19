import { afterEach, describe, expect, it, vi } from "vitest";

import {
  nativeArchiveDownloadUrl,
  nativeFileDownloadUrl,
  triggerNativeArchiveDownload,
  triggerNativeFileDownload,
} from "./nativeDownload";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("native download URLs", () => {
  it("encodes archive task ids", () => {
    expect(nativeArchiveDownloadUrl("task/with spaces")).toBe(
      "/api/download?taskId=task%2Fwith%20spaces",
    );
  });

  it("encodes direct file paths", () => {
    expect(nativeFileDownloadUrl("/tmp/image one.iso")).toBe(
      "/api/download?path=%2Ftmp%2Fimage%20one.iso",
    );
  });

  it("hands the URL to a temporary anchor without fetching it in JavaScript", () => {
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);

    triggerNativeArchiveDownload("task-1");

    expect(click).toHaveBeenCalledOnce();
    const link = click.mock.instances[0] as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe("/api/download?taskId=task-1");
    expect(link.download).toBe("");
    expect(document.body.contains(link)).toBe(false);
  });

  it("starts a direct file download without fetching in JavaScript", () => {
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);

    triggerNativeFileDownload("/tmp/image.iso");

    expect(click).toHaveBeenCalledOnce();
    const link = click.mock.instances[0] as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(
      "/api/download?path=%2Ftmp%2Fimage.iso",
    );
    expect(link.download).toBe("");
  });
});
