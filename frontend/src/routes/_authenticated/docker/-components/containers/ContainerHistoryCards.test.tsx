import { beforeEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { render, screen, waitFor } from "@/test/render";

import { ContainerHistoryCards } from "./ContainerHistoryCards";

const historyCalls = (request: { mock: { calls: unknown[][] } }) =>
  request.mock.calls.filter(
    ([handler, command]) =>
      handler === "monitoring" && command === "get_container_history",
  );

describe("ContainerHistoryCards", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("does not ask for container history without sudo", () => {
    const request = vi.spyOn(core, "request").mockResolvedValue([]);

    render(<ContainerHistoryCards containerId="alpha-id" />, {
      auth: { privileged: false },
      capabilities: { monitoringAvailable: true },
    });

    expect(
      screen.getAllByText(
        "Historical container monitoring requires a privileged session.",
      ),
    ).toHaveLength(4);
    expect(historyCalls(request)).toHaveLength(0);
  });

  it("reads container history with sudo", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue([]);

    render(<ContainerHistoryCards containerId="alpha-id" />, {
      auth: { privileged: true },
      capabilities: { monitoringAvailable: true },
    });

    await waitFor(() => {
      expect(historyCalls(request).length).toBeGreaterThan(0);
    });
    expect(
      screen.queryByText(
        "Historical container monitoring requires a privileged session.",
      ),
    ).not.toBeInTheDocument();
  });
});
