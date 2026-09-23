import { beforeEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { render, screen } from "@/test/render";

import ContainerActions from "./ContainerActions";

const callbacks = { onOpenLogs: vi.fn(), onOpenTerminal: vi.fn() };
const menuAt = { left: 0, top: 0 };

describe("ContainerActions", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    callbacks.onOpenLogs.mockClear();
    callbacks.onOpenTerminal.mockClear();
  });

  it("guards actions by state and confirms SIGKILL", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        contextMenu={menuAt}
        mode="card"
        name="example"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("button", { name: "Stop example" })).toBeEnabled();
    expect(screen.getByRole("menuitem", { name: "Pause" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Restart example" }),
    ).toBeEnabled();
    expect(screen.queryByRole("menuitem", { name: "Unpause" })).toBeNull();

    await user.click(screen.getByRole("menuitem", { name: "Kill" }));
    expect(
      screen.getByRole("dialog", { name: "Kill example?" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Kill container" }));

    expect(request).toHaveBeenCalledWith(
      "docker",
      "kill_container",
      { containerId: "running-id" },
      { retryPolicy: "none" },
    );
  });

  it("requires explicit force for an active removal", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        contextMenu={menuAt}
        mode="card"
        name="example"
        {...callbacks}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Remove example" }));
    const confirm = screen.getByRole("button", { name: "Remove container" });
    expect(confirm).toBeDisabled();

    await user.click(
      screen.getByRole("checkbox", {
        name: "Force removal of this active container",
      }),
    );
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    expect(request).toHaveBeenCalledWith(
      "docker",
      "remove_container",
      { containerId: "running-id", force: true },
      { retryPolicy: "none" },
    );
  });

  it.each([
    ["running", "Pause", "pause_container"],
    ["paused", "Unpause", "unpause_container"],
  ] as const)(
    "offers %s containers the %s action",
    async (state, label, route) => {
      const request = vi.spyOn(core, "request").mockResolvedValue(undefined);
      const { user } = render(
        <ContainerActions
          container={{ Id: "container-id", State: state }}
          contextMenu={menuAt}
          mode="card"
          name="example"
          {...callbacks}
        />,
      );

      await user.click(screen.getByRole("menuitem", { name: label }));

      expect(request).toHaveBeenCalledWith(
        "docker",
        route,
        { containerId: "container-id" },
        { retryPolicy: "none" },
      );
    },
  );

  it("offers only valid stopped-container lifecycle actions", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "stopped-id", State: "exited" }}
        contextMenu={menuAt}
        mode="card"
        name="stopped"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("button", { name: "Start stopped" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Restart stopped" }),
    ).toBeDisabled();
    expect(screen.queryByRole("menuitem", { name: "Pause" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Kill" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Remove stopped" }));
    expect(
      screen.queryByRole("checkbox", {
        name: "Force removal of this active container",
      }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "Remove container" }));

    expect(request).toHaveBeenCalledWith(
      "docker",
      "remove_container",
      { containerId: "stopped-id", force: false },
      { retryPolicy: "none" },
    );
  });

  it("directs Compose-managed containers to their stack editor", () => {
    render(
      <ContainerActions
        container={{
          Id: "compose-id",
          Labels: { "com.docker.compose.project": "example-stack" },
          State: "running",
        }}
        contextMenu={menuAt}
        mode="card"
        name="example"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("menuitem", { name: "Edit stack" })).toBeEnabled();
    expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();
  });

  it("keeps only lifecycle and removal icons, the rest in the menu", () => {
    render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        contextMenu={menuAt}
        mode="card"
        name="example"
        {...callbacks}
      />,
    );

    for (const label of ["Stop example", "Restart example", "Remove example"]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    expect(screen.getAllByRole("button")).toHaveLength(3);
    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Edit", "Pause", "Logs", "Terminal", "Kill"]);
  });
});
