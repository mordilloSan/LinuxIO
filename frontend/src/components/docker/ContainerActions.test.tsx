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
    expect(screen.getByRole("button", { name: "Pause" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Restart" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Unpause" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Kill" }));
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

    await user.click(screen.getByRole("button", { name: "Remove" }));
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

      await user.click(screen.getByRole("button", { name: label }));

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
    expect(screen.getByRole("button", { name: "Restart" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Kill" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Remove" }));
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
        mode="buttons"
        name="example"
        {...callbacks}
      />,
    );

    expect(screen.getByRole("button", { name: "Edit stack" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("lays every action out as its own labelled icon button", () => {
    render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        mode="icons-all"
        name="example"
        {...callbacks}
      />,
    );

    for (const label of [
      "Stop example",
      "Edit example",
      "Restart example",
      "Pause example",
      "Logs example",
      "Terminal example",
      "Kill example",
      "Remove example",
    ]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    expect(
      screen.queryByRole("button", { name: "Actions for example" }),
    ).toBeNull();
  });
});
