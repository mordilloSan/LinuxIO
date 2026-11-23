import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type Peer } from "@/api";
import * as core from "@/api/linuxio-core";
import {
  createTestQueryClient,
  render,
  screen,
  waitFor,
  within,
} from "@/test/render";

import InterfaceClients from "./InterfaceClients";

// The route's search is what the test sets; navigation is recorded.
const route = vi.hoisted(() => ({
  navigate: vi.fn(() => Promise.resolve()),
  search: {} as Record<string, unknown>,
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => route.navigate,
      useSearch: ({
        select,
      }: {
        select: (search: Record<string, unknown>) => unknown;
      }) => select(route.search),
    }),
  };
});

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

const peers: Peer[] = [
  {
    address: "10.0.0.2/32",
    client_allowed_ips: ["0.0.0.0/0"],
    dns: [],
    enabled: true,
    id: "Peer2",
    mtu: 0,
    name: "alice",
    public_key: "alice-key",
    server_allowed_ips: [],
  },
  {
    address: "10.0.0.3/32",
    client_allowed_ips: ["0.0.0.0/0"],
    dns: [],
    enabled: true,
    id: "Peer3",
    mtu: 0,
    name: "bob",
    public_key: "bob-key",
    server_allowed_ips: [],
  },
];

const QR = "data:image/png;base64,iVBORw0KGgo=";

/** The search the last navigation made from `previous`. */
function lastSearch(previous: Record<string, unknown>) {
  const call = route.navigate.mock.lastCall as unknown as
    | [{ search: (previous: Record<string, unknown>) => unknown }]
    | undefined;
  return call?.[0].search(previous);
}

function renderClients() {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(
    linuxio.wireguard.list_peers({ interfaceName: "wg0" }).queryKey,
    peers,
  );
  const requests: { command: string; request: unknown }[] = [];
  vi.spyOn(core, "request").mockImplementation((_handler, command, request) => {
    requests.push({ command, request });
    if (command === "list_peers") return Promise.resolve(peers);
    if (command === "peer_qrcode") return Promise.resolve({ qrcode: QR });
    if (command === "peer_config_download") {
      return Promise.resolve({ content: "[Interface]", filename: "a.conf" });
    }
    return Promise.resolve();
  });
  const view = render(<InterfaceClients params={{ id: "wg0" }} />, {
    queryClient,
  });
  return { requests, view };
}

describe("InterfaceClients QR code", () => {
  const originalCreateObjectURL = Object.getOwnPropertyDescriptor(
    window.URL,
    "createObjectURL",
  );
  const originalRevokeObjectURL = Object.getOwnPropertyDescriptor(
    window.URL,
    "revokeObjectURL",
  );

  beforeEach(() => {
    route.navigate.mockClear();
    route.search = { iface: "wg0" };
  });

  afterEach(() => {
    vi.restoreAllMocks();
    for (const [name, original] of [
      ["createObjectURL", originalCreateObjectURL],
      ["revokeObjectURL", originalRevokeObjectURL],
    ] as const) {
      if (original) Object.defineProperty(window.URL, name, original);
      else Reflect.deleteProperty(window.URL, name);
    }
  });

  it("opens the peer named in ?qr= with what to do with it", async () => {
    route.search = { iface: "wg0", qr: "Peer2" };
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    Object.defineProperty(window.URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:peer-config"),
    });
    Object.defineProperty(window.URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
    const { requests, view } = renderClients();

    const dialog = await screen.findByRole("dialog", {
      name: "Connect alice",
    });
    expect(
      within(dialog).getByText(
        "In the WireGuard app, tap + and choose Scan from QR code.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        "On a computer, download the configuration file and import it.",
      ),
    ).toBeInTheDocument();
    expect(
      await within(dialog).findByRole("img", { name: "QR code for alice" }),
    ).toHaveAttribute("src", QR);
    expect(
      requests.find((entry) => entry.command === "peer_qrcode")?.request,
    ).toEqual({ interfaceName: "wg0", peerId: "Peer2" });

    await view.user.click(
      within(dialog).getByRole("button", { name: "Download configuration" }),
    );
    await waitFor(() =>
      expect(
        requests.find((entry) => entry.command === "peer_config_download")
          ?.request,
      ).toEqual({ interfaceName: "wg0", peerId: "Peer2" }),
    );

    await view.user.click(within(dialog).getByRole("button", { name: "Done" }));
    expect(lastSearch(route.search)).toEqual({ iface: "wg0" });
  });

  it("opens a peer's code from its card through ?qr=", async () => {
    const { view } = renderClients();

    const bob = within(
      await screen.findByRole("group", { name: "Actions for bob" }),
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await view.user.click(bob.getByRole("button", { name: "View QR Code" }));

    expect(lastSearch(route.search)).toEqual({ iface: "wg0", qr: "Peer3" });
  });

  it("opens nothing for a peer the interface does not have", async () => {
    route.search = { iface: "wg0", qr: "Peer9" };
    const { requests } = renderClients();

    await screen.findByRole("group", { name: "Actions for alice" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(requests.some((entry) => entry.command === "peer_qrcode")).toBe(
      false,
    );
  });
});
