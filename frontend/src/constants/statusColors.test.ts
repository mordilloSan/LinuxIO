import { describe, expect, it } from "vitest";

import {
  getContainerStatusColor,
  SEMANTIC_STATUS_COLORS,
} from "./statusColors";

describe("getContainerStatusColor", () => {
  it.each([
    ["healthy", "success"],
    ["running", "success"],
    ["unhealthy", "warning"],
    ["starting", "warning"],
    ["paused", "warning"],
    ["restarting", "warning"],
    ["stopped", "error"],
    ["exited", "error"],
    ["dead", "error"],
    ["unknown", "warning"],
  ] as const)(
    "maps %s consistently for Docker and display states",
    (state, color) => {
      expect(getContainerStatusColor(state)).toBe(
        SEMANTIC_STATUS_COLORS[color],
      );
      expect(getContainerStatusColor(state.toUpperCase())).toBe(
        SEMANTIC_STATUS_COLORS[color],
      );
      expect(
        getContainerStatusColor(state[0].toUpperCase() + state.slice(1)),
      ).toBe(SEMANTIC_STATUS_COLORS[color]);
    },
  );
});
