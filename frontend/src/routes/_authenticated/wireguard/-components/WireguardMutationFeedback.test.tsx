import { afterEach, describe, expect, it, vi } from "vitest";

import { linuxio, type Peer, type WireGuardInterface } from "@/api";
import * as core from "@/api/linuxio-core";
import {
  act,
  createTestQueryClient,
  render,
  screen,
  waitFor,
  within,
} from "@/test/render";

import InterfaceClients from "./InterfaceClients";
import WireGuardDashboard from "./WireguardDashboard";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => vi.fn(),
      useSearch: () => null,
    }),
  };
});

const originalCreateObjectURL = Object.getOwnPropertyDescriptor(
  window.URL,
  "createObjectURL",
);
const originalRevokeObjectURL = Object.getOwnPropertyDescriptor(
  window.URL,
  "revokeObjectURL",
);

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

const wg0: WireGuardInterface = {
  address: "10.0.0.1/24",
  isConnected: "Inactive",
  isEnabled: false,
  name: "wg0",
  peerCount: 1,
  port: 51820,
};

const wg1: WireGuardInterface = {
  ...wg0,
  address: "10.1.0.1/24",
  isConnected: "Active",
  isEnabled: true,
  name: "wg1",
  port: 51821,
};

const peers: Peer[] = [
  {
    allowed_ips: ["10.0.0.2/32"],
    name: "alice",
    public_key: "alice-key",
  },
  {
    allowed_ips: ["10.0.0.3/32"],
    name: "bob",
    public_key: "bob-key",
  },
];

describe("WireGuard mutation feedback", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    if (originalCreateObjectURL) {
      Object.defineProperty(
        window.URL,
        "createObjectURL",
        originalCreateObjectURL,
      );
    } else {
      Reflect.deleteProperty(window.URL, "createObjectURL");
    }
    if (originalRevokeObjectURL) {
      Object.defineProperty(
        window.URL,
        "revokeObjectURL",
        originalRevokeObjectURL,
      );
    } else {
      Reflect.deleteProperty(window.URL, "revokeObjectURL");
    }
  });

  it("retains interface action ownership across status polling", async () => {
    const turningOn = createDeferred<void>();
    const turningOff = createDeferred<void>();
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        const name = (request as { name?: string } | undefined)?.name;
        if (command === "up_interface" && name === "wg0") {
          return turningOn.promise;
        }
        if (command === "down_interface" && name === "wg1") {
          return turningOff.promise;
        }
        if (command === "list_interfaces") {
          return Promise.resolve([wg0, wg1]);
        }
        return Promise.resolve();
      },
    );
    const view = render(<WireGuardDashboard interfaces={[wg0, wg1]} />);
    const wg0Actions = within(
      screen.getByRole("group", { name: "Actions for wg0" }),
    );
    const wg1Actions = within(
      screen.getByRole("group", { name: "Actions for wg1" }),
    );

    await view.user.click(
      wg0Actions.getByRole("button", { name: "Turn interface on" }),
    );

    const turningOnButton = await wg0Actions.findByRole("button", {
      name: "Turning interface on wg0",
    });
    expect(
      within(turningOnButton).getByRole("progressbar"),
    ).toBeInTheDocument();
    expect(wg0Actions.getByRole("button", { name: "Add peer" })).toBeDisabled();
    expect(
      wg1Actions.getByRole("button", { name: "Turn interface off" }),
    ).toBeEnabled();

    view.rerender(
      <WireGuardDashboard
        interfaces={[{ ...wg0, isConnected: "Active" }, wg1]}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Turning interface on wg0" }),
    ).toBeInTheDocument();

    await view.user.click(
      wg1Actions.getByRole("button", { name: "Turn interface off" }),
    );
    expect(
      within(
        await wg1Actions.findByRole("button", {
          name: "Turning interface off wg1",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();

    await act(async () => {
      turningOn.resolve(undefined);
      await turningOn.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Turning interface on wg0" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Turning interface off wg1" }),
    ).toBeInTheDocument();

    await act(async () => {
      turningOff.resolve(undefined);
      await turningOff.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Turning interface off wg1" }),
      ).not.toBeInTheDocument();
    });
  });

  it("keeps peer actions scoped while delete and download settle independently", async () => {
    const deleting = createDeferred<void>();
    const downloading = createDeferred<{ content: string; filename: string }>();
    const queryClient = createTestQueryClient();
    const peersQuery = linuxio.wireguard.list_peers({ interfaceName: "wg0" });
    queryClient.setQueryData(peersQuery.queryKey, peers);
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    Object.defineProperty(window.URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:peer-config"),
    });
    Object.defineProperty(window.URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        const peerName = (request as { peerName?: string } | undefined)
          ?.peerName;
        if (command === "remove_peer" && peerName === "alice") {
          return deleting.promise;
        }
        if (command === "peer_config_download" && peerName === "bob") {
          return downloading.promise;
        }
        if (command === "list_peers") {
          return Promise.resolve(peers);
        }
        return Promise.resolve();
      },
    );
    const view = render(<InterfaceClients params={{ id: "wg0" }} />, {
      queryClient,
    });
    const aliceActions = within(
      await screen.findByRole("group", { name: "Actions for alice" }),
    );
    const bobActions = within(
      screen.getByRole("group", { name: "Actions for bob" }),
    );

    await view.user.click(aliceActions.getByRole("button", { name: "Delete" }));

    expect(
      within(
        await aliceActions.findByRole("button", {
          name: "Deleting peer alice",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();
    expect(
      aliceActions.getByRole("button", { name: "Download Config" }),
    ).toBeDisabled();
    expect(
      bobActions.getByRole("button", { name: "Download Config" }),
    ).toBeEnabled();

    await view.user.click(
      bobActions.getByRole("button", { name: "Download Config" }),
    );
    expect(
      within(
        await bobActions.findByRole("button", {
          name: "Downloading config for bob",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();

    await act(async () => {
      deleting.resolve(undefined);
      await deleting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Deleting peer alice" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Downloading config for bob" }),
    ).toBeInTheDocument();

    await act(async () => {
      downloading.resolve({ content: "[Interface]", filename: "bob.conf" });
      await downloading.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", {
          name: "Downloading config for bob",
        }),
      ).not.toBeInTheDocument();
    });
  });
});
