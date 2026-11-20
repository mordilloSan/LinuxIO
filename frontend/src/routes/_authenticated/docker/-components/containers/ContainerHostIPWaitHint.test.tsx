import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  ContainerPortBinding,
  DockerHostIPs,
  DockerSettings,
} from "@/api";
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

import ContainerHostIPWaitHint from "./ContainerHostIPWaitHint";

const port = (hostIp: string, hostPort = "53"): ContainerPortBinding => ({
  containerPort: 53,
  hostIp,
  hostPort,
  protocol: "udp",
});

const dnsPorts = [
  port("192.168.1.66"),
  { ...port("192.168.1.66"), protocol: "tcp" },
];

function renderHint(
  ports: ContainerPortBinding[],
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
      <ContainerHostIPWaitHint ports={ports} />
    </ConfigContext.Provider>,
    { auth, queryClient, seedConfig: false },
  );
}

const settle = () => new Promise((resolve) => setTimeout(resolve));

describe("ContainerHostIPWaitHint", () => {
  beforeEach(() => {
    mocks.hostIPs = {
      assigned: ["192.168.1.66"],
      published: ["192.168.1.66"],
    };
    mocks.listHostIPs.mockClear();
    mocks.updateConfig.mockClear();
  });

  it("offers to wait for the address the ports are published on", async () => {
    const { user } = renderHint(dnsPorts);

    expect(
      await screen.findByText(/Docker does not wait for 192\.168\.1\.66/),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "Make Docker wait for 192.168.1.66 at boot",
      }),
    );

    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
    expect(mocks.updateConfig).toHaveBeenCalledWith(
      { docker: { waitForHostIPs: true, hostIPs: ["192.168.1.66"] } },
      expect.any(Function),
    );
  });

  it("adds to the addresses Docker already waits for", async () => {
    const { user } = renderHint(dnsPorts, {
      hostIPs: ["10.0.0.9"],
      waitForHostIPs: true,
    });

    await user.click(
      await screen.findByRole("button", { name: /Make Docker wait/ }),
    );

    expect(mocks.updateConfig).toHaveBeenCalledWith(
      {
        docker: { waitForHostIPs: true, hostIPs: ["10.0.0.9", "192.168.1.66"] },
      },
      expect.any(Function),
    );
  });

  it("turns the setting back on without listing a stored address twice", async () => {
    const { user } = renderHint(dnsPorts, {
      hostIPs: ["192.168.1.66"],
      waitForHostIPs: false,
    });

    await user.click(
      await screen.findByRole("button", { name: /Make Docker wait/ }),
    );

    expect(mocks.updateConfig).toHaveBeenCalledWith(
      { docker: { waitForHostIPs: true, hostIPs: ["192.168.1.66"] } },
      expect.any(Function),
    );
  });

  it("leaves out a stored address the server no longer holds when it turns the setting on", async () => {
    mocks.hostIPs = {
      assigned: ["192.168.1.66", "10.0.0.8"],
      published: ["192.168.1.66"],
    };
    const { user } = renderHint(dnsPorts, {
      hostIPs: ["10.0.0.8", "10.0.0.9"],
      waitForHostIPs: false,
    });

    await user.click(
      await screen.findByRole("button", { name: /Make Docker wait/ }),
    );

    expect(mocks.updateConfig).toHaveBeenCalledWith(
      {
        docker: { waitForHostIPs: true, hostIPs: ["10.0.0.8", "192.168.1.66"] },
      },
      expect.any(Function),
    );
  });

  it("says so, and asks nothing, when Docker already waits for the address", async () => {
    renderHint(dnsPorts, { hostIPs: ["192.168.1.66"], waitForHostIPs: true });

    expect(
      screen.getByText("Docker waits for 192.168.1.66 at boot."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    await settle();
    expect(mocks.listHostIPs).not.toHaveBeenCalled();
  });

  it("matches an address whatever its spelling", () => {
    renderHint([port("2001:DB8:0::66")], {
      hostIPs: ["2001:db8::66"],
      waitForHostIPs: true,
    });

    expect(
      screen.getByText("Docker waits for 2001:db8::66 at boot."),
    ).toBeInTheDocument();
  });

  it("shows nothing for ports on every address, on loopback or not published", async () => {
    const { container } = renderHint([
      port("0.0.0.0"),
      port("::"),
      port("127.0.0.1"),
      port("", ""),
      port("192.168.1.66", ""),
    ]);

    await settle();
    expect(container).toBeEmptyDOMElement();
    expect(mocks.listHostIPs).not.toHaveBeenCalled();
  });

  it("does not offer an address the server does not hold", async () => {
    // 192.168.1.67 is on no interface. 172.17.0.1 is a Docker network's
    // gateway: the bridge leaves it out of both lists, and it is not offered
    // when a failed read of Docker left it among the assigned ones.
    mocks.hostIPs = {
      assigned: ["192.168.1.66", "172.17.0.1"],
      published: ["192.168.1.67"],
    };
    const { container } = renderHint([
      port("172.17.0.1"),
      port("192.168.1.67"),
    ]);

    await settle();
    expect(mocks.listHostIPs).toHaveBeenCalled();
    expect(container).toBeEmptyDOMElement();
  });

  it("shows nothing to a session that is not privileged", async () => {
    const { container } = renderHint(dnsPorts, {}, false);

    await settle();
    expect(container).toBeEmptyDOMElement();
    expect(mocks.listHostIPs).not.toHaveBeenCalled();
  });
});
