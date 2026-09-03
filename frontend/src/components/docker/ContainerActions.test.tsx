import { beforeEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { render, screen } from "@/test/render";

import ContainerActions from "./ContainerActions";

const callbacks = { onOpenLogs: vi.fn(), onOpenTerminal: vi.fn() };

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
        mode="buttons"
        name="example"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("button", { name: "Stop example" })).toBeEnabled();
    await user.click(
      screen.getByRole("button", { name: "Actions for example" }),
    );
    expect(screen.getByRole("menuitem", { name: "Pause" })).toBeEnabled();
    expect(screen.getByRole("menuitem", { name: "Restart" })).toBeEnabled();
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
        mode="buttons"
        name="example"
        {...callbacks}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Actions for example" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
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
          mode="buttons"
          name="example"
          {...callbacks}
        />,
      );

      await user.click(
        screen.getByRole("button", { name: "Actions for example" }),
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
        mode="buttons"
        name="stopped"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("button", { name: "Start stopped" })).toBeEnabled();
    await user.click(
      screen.getByRole("button", { name: "Actions for stopped" }),
    );
    expect(screen.getByRole("menuitem", { name: "Restart" })).toBeDisabled();
    expect(screen.queryByRole("menuitem", { name: "Pause" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Kill" })).toBeNull();
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
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
});
