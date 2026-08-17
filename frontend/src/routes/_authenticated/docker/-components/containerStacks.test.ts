import { describe, expect, it } from "vitest";

import type { ContainerInfo } from "@/api";

import {
  buildContainerTableRows,
  getStackDisplayState,
  groupContainersByStack,
  isStackHeaderRow,
  summarizeStack,
} from "./containerStacks";

function container(
  id: string,
  project?: string,
  overrides: Partial<ContainerInfo> = {},
): ContainerInfo {
  return {
    Created: 1,
    Id: id,
    Image: "alpine:latest",
    Names: [`/${id}`],
    State: "running",
    Status: "Up 1 minute",
    ...(project === undefined
      ? {}
      : { Labels: { "com.docker.compose.project": project } }),
    ...overrides,
  };
}

describe("groupContainersByStack", () => {
  it("groups multi-container projects where their first member sits, keeping member order", () => {
    const entries = groupContainersByStack([
      container("media-web", "media"),
      container("plain"),
      container("media-db", "media"),
    ]);

    expect(entries).toEqual([
      {
        type: "stack",
        project: "media",
        containers: [
          expect.objectContaining({ Id: "media-web" }),
          expect.objectContaining({ Id: "media-db" }),
        ],
      },
      {
        type: "container",
        container: expect.objectContaining({ Id: "plain" }),
      },
    ]);
  });

  it("leaves single-container projects and blank labels loose", () => {
    const entries = groupContainersByStack([
      container("solo-app", "solo"),
      container("blank", "   "),
    ]);

    expect(entries.every((entry) => entry.type === "container")).toBe(true);
  });
});

describe("buildContainerTableRows", () => {
  const containers = [
    container("media-web", "media"),
    container("media-db", "media"),
    container("plain"),
  ];

  it("interleaves an expanded header row above its members", () => {
    const rows = buildContainerTableRows(containers, new Set());

    expect(
      rows.map((row) => (isStackHeaderRow(row) ? "header" : row.Id)),
    ).toEqual(["header", "media-web", "media-db", "plain"]);
    expect(rows[0]).toMatchObject({ collapsed: false, project: "media" });
  });

  it("folds a collapsed stack down to its header row", () => {
    const rows = buildContainerTableRows(containers, new Set(["media"]));

    expect(
      rows.map((row) => (isStackHeaderRow(row) ? "header" : row.Id)),
    ).toEqual(["header", "plain"]);
    expect(rows[0]).toMatchObject({ collapsed: true, project: "media" });
  });
});

describe("stack summaries", () => {
  it("reports counts and maps them onto display states", () => {
    const running = [container("a", "s"), container("b", "s")];
    expect(getStackDisplayState(summarizeStack(running))).toBe("Running");

    const mixed = [
      container("a", "s"),
      container("b", "s", { State: "exited", Status: "Exited" }),
    ];
    const mixedSummary = summarizeStack(mixed);
    expect(mixedSummary).toMatchObject({ running: 1, total: 2 });
    expect(getStackDisplayState(mixedSummary)).toBe("Partially running");

    const stopped = [
      container("a", "s", { State: "exited", Status: "Exited" }),
    ];
    expect(getStackDisplayState(summarizeStack(stopped))).toBe("Stopped");

    const unhealthy = [
      container("a", "s", { Status: "Up 1 minute (unhealthy)" }),
      container("b", "s"),
    ];
    expect(getStackDisplayState(summarizeStack(unhealthy))).toBe("Unhealthy");
  });

  it("surfaces a pending update from any member", () => {
    const summary = summarizeStack([
      container("a", "s"),
      container("b", "s", { updateAvailable: true }),
    ]);
    expect(summary.updateAvailable).toBe(true);
  });
});
