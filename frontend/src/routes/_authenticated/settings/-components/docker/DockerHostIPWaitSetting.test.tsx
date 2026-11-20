import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { DockerHostIPs, DockerSettings } from "@/api";
import { bridgeConfigQueryKey } from "@/api/config-query";
import { cacheUserId } from "@/api/user-scope";
import { ConfigContext } from "@/contexts/ConfigContext";
import {
  buildTestConfigSnapshots,
  createAuthContextValue,
  createConfigContextValue,
  createTestQueryClient,
  render,
} from "@/test/render";

const mocks = vi.hoisted(() => ({
  hostIPs: { assigned: [], published: [] } as DockerHostIPs,
  listHostIPs: vi.fn(),
  updateConfig: vi.fn(),
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: vi.fn(async (route: string) => {
    if (route === "docker.list_host_ips") {
      mocks.listHostIPs();
      return mocks.hostIPs;
    }
    throw new Error("the bridge did not answer");
  }),
}));

import DockerHostIPWaitSetting from "./DockerHostIPWaitSetting";

const NOT_ASSIGNED =
  "Not assigned on this host now. Docker waits 30 s for it at every boot.";

function renderSetting(
  docker: Partial<DockerSettings> = {},
  privileged = true,
) {
  const auth = { privileged };
  const queryClient = createTestQueryClient();
  const { bridge } = buildTestConfigSnapshots();
  queryClient.setQueryData(
    bridgeConfigQueryKey(cacheUserId(createAuthContextValue(auth).user)),
    { ...bridge, docker: { ...bridge.docker, ...docker } },
  );
  return render(
    <ConfigContext.Provider
      value={createConfigContextValue({ updateConfig: mocks.updateConfig })}
    >
      <DockerHostIPWaitSetting />
    </ConfigContext.Provider>,
    { auth, queryClient, seedConfig: false },
  );
}

const toggle = () =>
  screen.getByRole("checkbox", { name: "Wait for published host IPs" });
const rowValues = () =>
  screen
    .getAllByRole("textbox")
    .map((field) => (field as HTMLInputElement).value);

describe("DockerHostIPWaitSetting", () => {
  beforeEach(() => {
    mocks.hostIPs = { assigned: [], published: [] };
    mocks.listHostIPs.mockClear();
    mocks.updateConfig.mockClear();
  });

  it("hides the list and asks for no suggestions while off", async () => {
    renderSetting({ hostIPs: ["192.168.1.66"] });

    expect(toggle()).not.toBeChecked();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument();
    await Promise.resolve();
    expect(mocks.listHostIPs).not.toHaveBeenCalled();
  });

  it("pre-fills an empty list from the published IPs the host holds and saves both fields", async () => {
    mocks.hostIPs = {
      assigned: ["192.168.1.66", "2001:db8::66", "10.0.0.9"],
      published: ["192.168.1.66", "2001:db8::66"],
    };
    const { user } = renderSetting();

    await user.click(toggle());
    await waitFor(() =>
      expect(rowValues()).toEqual(["192.168.1.66", "2001:db8::66"]),
    );
    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.queryByText(NOT_ASSIGNED)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
    expect(mocks.updateConfig).toHaveBeenCalledWith(
      {
        docker: {
          waitForHostIPs: true,
          hostIPs: ["192.168.1.66", "2001:db8::66"],
        },
      },
      expect.any(Function),
    );
  });

  it("does not pre-fill a published IP the host does not hold, and offers it with a note", async () => {
    mocks.hostIPs = {
      assigned: ["192.168.1.66"],
      published: ["192.168.1.66", "192.168.1.67"],
    };
    const { user } = renderSetting();

    await user.click(toggle());
    await waitFor(() => expect(rowValues()).toEqual(["192.168.1.66"]));

    expect(screen.getByText("192.168.1.67")).toBeInTheDocument();
    expect(screen.getByText(NOT_ASSIGNED)).toBeInTheDocument();
  });

  it("adds a detected IP that is not in the list", async () => {
    mocks.hostIPs = {
      assigned: ["192.168.1.66", "192.168.1.67"],
      published: ["192.168.1.66", "192.168.1.67"],
    };
    const { user } = renderSetting({
      hostIPs: ["192.168.1.66"],
      waitForHostIPs: true,
    });

    await user.click(
      await screen.findByRole("button", { name: "Add 192.168.1.67" }),
    );

    expect(rowValues()).toEqual(["192.168.1.66", "192.168.1.67"]);
    expect(
      screen.queryByRole("button", { name: "Add 192.168.1.67" }),
    ).not.toBeInTheDocument();
    expect(mocks.updateConfig).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(mocks.updateConfig).toHaveBeenCalledWith(
      {
        docker: {
          waitForHostIPs: true,
          hostIPs: ["192.168.1.66", "192.168.1.67"],
        },
      },
      expect.any(Function),
    );
  });

  it("marks a listed IP the host does not hold and still saves it", async () => {
    mocks.hostIPs = { assigned: ["192.168.1.66"], published: [] };
    const { user } = renderSetting({
      hostIPs: ["192.168.1.66", "2001:db8::66"],
      waitForHostIPs: true,
    });

    expect(await screen.findAllByText(NOT_ASSIGNED)).toHaveLength(1);

    await user.type(screen.getByLabelText("IP address 2"), "6");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.updateConfig).toHaveBeenCalledWith(
      {
        docker: {
          waitForHostIPs: true,
          hostIPs: ["192.168.1.66", "2001:db8::666"],
        },
      },
      expect.any(Function),
    );
  });

  it("sends the canonical form of what was typed", async () => {
    const { user } = renderSetting();

    await user.click(toggle());
    await user.type(screen.getByLabelText("IP address"), " 2001:DB8:0::66 ");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.updateConfig).toHaveBeenCalledWith(
      { docker: { waitForHostIPs: true, hostIPs: ["2001:db8::66"] } },
      expect.any(Function),
    );
  });

  it("shows a row error and does not save an invalid or empty list", async () => {
    const { user } = renderSetting();

    await user.click(toggle());
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByText("IP address is required.")).toBeInTheDocument();

    await user.type(screen.getByLabelText("IP address"), "127.0.0.1");
    expect(
      screen.queryByText("IP address is required."),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(
      screen.getByText("A loopback address is always assigned."),
    ).toBeInTheDocument();

    expect(mocks.updateConfig).not.toHaveBeenCalled();
  });

  it("sends only the switch when turned off, keeping the stored list", async () => {
    const { user } = renderSetting({
      hostIPs: ["192.168.1.66"],
      waitForHostIPs: true,
    });

    await user.click(toggle());
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
    expect(mocks.updateConfig).toHaveBeenCalledWith(
      { docker: { waitForHostIPs: false } },
      expect.any(Function),
    );
  });

  it("has nothing to save when rows were edited and the switch is back off", async () => {
    const { user } = renderSetting();

    await user.click(toggle());
    await user.type(screen.getByLabelText("IP address"), "10.0.0.2");
    await user.click(toggle());

    expect(
      screen.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument();
    expect(mocks.updateConfig).not.toHaveBeenCalled();
  });

  it("resets the draft to what is stored", async () => {
    const { user } = renderSetting({
      hostIPs: ["192.168.1.66"],
      waitForHostIPs: true,
    });

    await user.click(screen.getByRole("button", { name: /^Add IP address/ }));
    await user.type(screen.getByLabelText("IP address 2"), "10.0.0.2");
    await user.click(screen.getByRole("button", { name: "Reset" }));

    expect(rowValues()).toEqual(["192.168.1.66"]);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("is disabled for a session that is not privileged", () => {
    renderSetting({ hostIPs: ["192.168.1.66"], waitForHostIPs: true }, false);

    expect(toggle()).toBeDisabled();
    expect(screen.getByLabelText("IP address")).toBeDisabled();
    expect(
      screen.getByRole("button", { name: /^Add IP address/ }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(
      screen.getByText(
        "Privileged mode is required to change Docker startup ordering.",
      ),
    ).toBeInTheDocument();
  });
});
