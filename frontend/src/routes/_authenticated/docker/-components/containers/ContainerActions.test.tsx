import { beforeEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { render, screen } from "@/test/render";

import { DockerUpdateOperationProvider } from "../shared/DockerUpdateOperationProvider";
import ContainerActions from "./ContainerActions";

const callbacks = { onOpenLogs: vi.fn(), onOpenTerminal: vi.fn() };

describe("ContainerActions", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    callbacks.onOpenLogs.mockClear();
    callbacks.onOpenTerminal.mockClear();
  });

  it("guards actions by state and confirms SIGKILL", async () => {
    const request = vi
      .spyOn(core, "mutationRequest")
      .mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        mode="card"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: true } },
    );

    expect(screen.getByRole("button", { name: "Stop example" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Pause example" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Restart example" }),
    ).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: "Unpause example" }),
    ).toBeNull();

    await user.click(screen.getByRole("button", { name: "Kill example" }));
    expect(
      screen.getByRole("dialog", { name: "Kill example?" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Kill container" }));

    expect(request).toHaveBeenCalledWith(
      "docker",
      "kill_container",
      { containerId: "running-id" },
      {
        onProgress: expect.any(Function),
        onSnapshot: expect.any(Function),
      },
    );
  });

  it("requires explicit force for an active removal", async () => {
    const request = vi
      .spyOn(core, "mutationRequest")
      .mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        mode="card"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: true } },
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
      {
        onProgress: expect.any(Function),
        onSnapshot: expect.any(Function),
      },
    );
  });

  it.each([
    ["running", "Pause", "pause_container"],
    ["paused", "Unpause", "unpause_container"],
  ] as const)(
    "offers %s containers the %s action",
    async (state, label, route) => {
      const request = vi
        .spyOn(core, "mutationRequest")
        .mockResolvedValue(undefined);
      const { user } = render(
        <ContainerActions
          container={{ Id: "container-id", State: state }}
          mode="card"
          name="example"
          {...callbacks}
        />,
        { auth: { privileged: true } },
      );

      await user.click(
        screen.getByRole("button", { name: `${label} example` }),
      );

      expect(request).toHaveBeenCalledWith(
        "docker",
        route,
        { containerId: "container-id" },
        {
          onProgress: expect.any(Function),
          onSnapshot: expect.any(Function),
        },
      );
    },
  );

  it("offers only valid stopped-container lifecycle actions", async () => {
    const request = vi
      .spyOn(core, "mutationRequest")
      .mockResolvedValue(undefined);
    const { user } = render(
      <ContainerActions
        container={{ Id: "stopped-id", State: "exited" }}
        mode="card"
        name="stopped"
        {...callbacks}
      />,
      { auth: { privileged: true } },
    );

    expect(screen.getByRole("button", { name: "Start stopped" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Restart stopped" }),
    ).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Pause stopped" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Kill stopped" })).toBeNull();
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
      {
        onProgress: expect.any(Function),
        onSnapshot: expect.any(Function),
      },
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
        mode="full"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: true } },
    );

    expect(
      screen.getByRole("button", { name: "Edit stack example" }),
    ).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Edit example" })).toBeNull();
  });

  it("leaves Edit and Update to the details card in card mode", () => {
    render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        mode="card"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: true } },
    );

    expect(
      screen.getByRole("button", { name: "Remove example" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: /^Edit/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Update/ })).toBeNull();
  });

  it("offers Update in full mode to a session with sudo", () => {
    render(
      <DockerUpdateOperationProvider>
        <ContainerActions
          container={{
            Id: "running-id",
            State: "running",
            updateAvailable: true,
          }}
          mode="full"
          name="example"
          {...callbacks}
        />
      </DockerUpdateOperationProvider>,
      { auth: { privileged: true } },
    );

    expect(
      screen.getByRole("button", { name: "Update example" }),
    ).toBeEnabled();
  });

  it("offers only logs and the app link without sudo", async () => {
    const container = {
      Id: "running-id",
      Labels: { "com.docker.compose.project": "example-stack" },
      State: "running",
      updateAvailable: true,
      url: "http://example.test",
    };
    const { unmount } = render(
      <DockerUpdateOperationProvider>
        <ContainerActions
          container={container}
          mode="full"
          name="example"
          {...callbacks}
        />
      </DockerUpdateOperationProvider>,
      { auth: { privileged: false } },
    );

    expect(
      screen
        .getAllByRole("button")
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["Logs example", "Open App example"]);
    unmount();

    const { user } = render(
      <ContainerActions
        container={container}
        mode="menu"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: false } },
    );
    await user.click(
      screen.getByRole("button", { name: "Actions for example" }),
    );

    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Logs", "Open App"]);
  });

  it("shows every action as an icon in full mode", () => {
    render(
      <ContainerActions
        container={{ Id: "running-id", State: "running" }}
        mode="full"
        name="example"
        {...callbacks}
      />,
      { auth: { privileged: true } },
    );

    expect(
      screen
        .getAllByRole("button")
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual([
      "Stop example",
      "Edit example",
      "Restart example",
      "Pause example",
      "Logs example",
      "Terminal example",
      "Kill example",
      "Remove example",
    ]);
    expect(screen.queryByRole("menu")).toBeNull();
  });
});
