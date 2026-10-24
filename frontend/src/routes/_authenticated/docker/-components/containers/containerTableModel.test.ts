import { describe, expect, it } from "vitest";

import type { ContainerInfo } from "@/api";

import {
  areContainerArraysEquivalent,
  getUpdateStatus,
  getVersionDisplay,
  getVisibleEntries,
  isCollapsible,
} from "./containerTableModel";

const container = (overrides: Partial<ContainerInfo>): ContainerInfo =>
  ({
    Id: "abc",
    Image: "nginx:1.25",
    Names: ["/web"],
    State: "running",
    Status: "Up",
    ...overrides,
  }) as ContainerInfo;

describe("getVersionDisplay", () => {
  it("uses the image tag", () => {
    expect(getVersionDisplay(container({}))).toBe("1.25");
  });

  it("falls back to the labelled version for untagged images", () => {
    expect(
      getVersionDisplay(
        container({ Image: "nginx", Labels: { version: "2.0.1" } }),
      ),
    ).toBe("2.0.1");
  });

  it("pairs a floating latest tag with the labelled version", () => {
    expect(
      getVersionDisplay(
        container({
          Image: "nginx:latest@sha256:abc",
          Labels: { "org.opencontainers.image.version": "2.0.1" },
        }),
      ),
    ).toBe("latest (2.0.1)");
  });
});

describe("getUpdateStatus", () => {
  it("reports failed scans before anything else", () => {
    expect(getUpdateStatus({ updateError: "boom" })).toMatchObject({
      color: "error",
      label: "Scan failed",
    });
  });

  it("reports an available update", () => {
    expect(getUpdateStatus({ updateAvailable: true })).toMatchObject({
      color: "warning",
      label: "Update available",
    });
  });

  it("reports containers that were never scanned", () => {
    expect(getUpdateStatus({})).toMatchObject({
      detail: "Never scanned",
      label: "Not scanned",
    });
  });
});

describe("collapsible entries", () => {
  it("keeps up to three entries fully visible", () => {
    expect(isCollapsible(3)).toBe(false);
    expect(getVisibleEntries(3)).toBe(3);
    expect(isCollapsible(4)).toBe(true);
    expect(getVisibleEntries(4)).toBe(2);
  });
});

describe("areContainerArraysEquivalent", () => {
  it("compares rendered fields, not references", () => {
    expect(areContainerArraysEquivalent([container({})], [container({})])).toBe(
      true,
    );
    expect(
      areContainerArraysEquivalent(
        [container({})],
        [container({ State: "exited" })],
      ),
    ).toBe(false);
  });
});
