import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Peer, WireGuardInterface } from "@/api";
import * as core from "@/api/linuxio-core";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen, waitFor, within } from "@/test/render";

import CreateInterfaceButton from "./CreateInterfaceButton";

const navigate = vi.hoisted(() => vi.fn(() => Promise.resolve()));

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({ useNavigate: () => navigate }),
  };
});

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

const firstPeer: Peer = {
  address: "10.10.20.2/32",
  client_allowed_ips: ["0.0.0.0/0"],
  dns: [],
  enabled: true,
  id: "wg0-peer1",
  mtu: 0,
  name: "peer1",
  public_key: "peer1-key",
  server_allowed_ips: [],
};

const wg0: WireGuardInterface = {
  address: "10.10.20.0/24",
  dns: [],
  forwardingOff: false,
  hooks: { PreUp: [], PostUp: [], PreDown: [], PostDown: [] },
  host: "",
  mtu: 0,
  isConnected: "Active",
  isEnabled: true,
  name: "wg0",
  peerCount: 1,
  port: 51820,
};

interface Backend {
  creates: unknown[];
  peerReads: string[];
}

/** Answers the routes the create workflow uses; `nics` is the network list. */
function fakeBackend({
  nics = [testNetworkInterface({ ipv4: ["192.168.1.2/24"], name: "eth0" })],
  peers = () => Promise.resolve([firstPeer]),
}: {
  nics?: ReturnType<typeof testNetworkInterface>[];
  peers?: () => Promise<Peer[]>;
} = {}): Backend {
  const backend: Backend = { creates: [], peerReads: [] };
  vi.spyOn(core, "request").mockImplementation((_handler, command, request) => {
    if (command === "get_network_info") return Promise.resolve(nics);
    if (command === "list_peers") {
      backend.peerReads.push(
        (request as { interfaceName: string }).interfaceName,
      );
      return peers();
    }
    return Promise.resolve();
  });
  vi.spyOn(core, "mutationRequest").mockImplementation(
    (_handler, command, request) => {
      if (command === "add_interface") backend.creates.push(request);
      return Promise.resolve();
    },
  );
  return backend;
}

/** The search the last navigation made, applied to an empty search. */
function lastSearch() {
  const call = navigate.mock.lastCall as unknown as
    | [{ search: (previous: Record<string, unknown>) => unknown }]
    | undefined;
  return call?.[0].search({});
}

async function openDialog(view: ReturnType<typeof render>) {
  await view.user.click(
    screen.getByRole("button", { name: "Create New Interface" }),
  );
  const dialog = await screen.findByRole("dialog", {
    name: "Create New Interface",
  });
  // The NIC is known once the network list is in.
  await within(dialog).findByText("Port 51820 · 10.10.20.0/24 · through eth0");
  return dialog;
}

describe("CreateInterfaceButton", () => {
  beforeEach(() => {
    navigate.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("asks for a name and devices, and keeps the rest in a closed Advanced", async () => {
    fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);

    const dialog = await openDialog(view);

    expect(within(dialog).getByLabelText("Interface Name")).toHaveValue("wg0");
    expect(within(dialog).getByLabelText("Devices")).toHaveValue(1);
    expect(
      within(dialog).getByText(
        "One configuration is made for each. You can add more later.",
      ),
    ).toBeInTheDocument();
    const advanced = within(dialog).getByRole("button", { name: "Advanced" });
    expect(advanced).toHaveAttribute("aria-expanded", "false");
    expect(within(dialog).queryByLabelText("Port")).not.toBeInTheDocument();
    expect(within(dialog).queryByLabelText("CIDR")).not.toBeInTheDocument();

    await view.user.click(advanced);

    expect(advanced).toHaveAttribute("aria-expanded", "true");
    const panel = document.getElementById(
      advanced.getAttribute("aria-controls") ?? "",
    );
    expect(panel).not.toBeNull();
    expect(within(panel!).getByLabelText("Port")).toHaveValue(51820);
    expect(within(panel!).getByLabelText("CIDR")).toHaveValue("10.10.20.0/24");
    expect(
      within(panel!).getByRole("combobox", { name: "NIC" }),
    ).toHaveTextContent("eth0 (192.168.1.2/24)");
  });

  it("names the DNS, endpoint host and MTU in the summary once set", async () => {
    fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);

    await view.user.click(
      within(dialog).getByRole("button", { name: "Advanced" }),
    );
    await view.user.type(
      within(dialog).getByLabelText("DNS (optional)"),
      "1.1.1.1, ",
    );
    await view.user.type(
      within(dialog).getByLabelText("Endpoint host (optional)"),
      "vpn.example.org",
    );
    await view.user.type(
      within(dialog).getByLabelText("MTU (optional)"),
      "1400",
    );

    expect(
      within(dialog).getByText(
        "Port 51820 · 10.10.20.0/24 · through eth0 · DNS 1.1.1.1 · endpoint vpn.example.org · MTU 1400",
      ),
    ).toBeInTheDocument();
  });

  it("shows Advanced open while a field in it is invalid", async () => {
    fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[wg0]} />);
    const dialog = await view.user
      .click(screen.getByRole("button", { name: "Create New Interface" }))
      .then(() =>
        screen.findByRole("dialog", { name: "Create New Interface" }),
      );
    await within(dialog).findByText(
      "Port 51821 · 10.10.30.0/24 · through eth0",
    );
    const advanced = within(dialog).getByRole("button", { name: "Advanced" });

    await view.user.click(advanced);
    const port = within(dialog).getByLabelText("Port");
    await view.user.clear(port);
    await view.user.type(port, "51820");
    // Closing is refused while the port is taken: its message stays in view.
    await view.user.click(advanced);

    expect(advanced).toHaveAttribute("aria-expanded", "true");
    expect(
      within(dialog).getByText("This port is already in use."),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Create Interface" }),
    ).toBeDisabled();

    // Fixing it does not fold the section away; closing works again.
    await view.user.clear(port);
    await view.user.type(port, "51822");
    expect(advanced).toHaveAttribute("aria-expanded", "true");
    await view.user.click(advanced);
    expect(advanced).toHaveAttribute("aria-expanded", "false");
    expect(
      within(dialog).getByText("Port 51822 · 10.10.30.0/24 · through eth0"),
    ).toBeInTheDocument();
  });

  it("keeps Advanced open while the CIDR is empty", async () => {
    fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);
    const advanced = within(dialog).getByRole("button", { name: "Advanced" });

    await view.user.click(advanced);
    await view.user.clear(within(dialog).getByLabelText("CIDR"));
    await view.user.click(advanced);

    expect(advanced).toHaveAttribute("aria-expanded", "true");
    expect(within(dialog).getByLabelText("CIDR")).toHaveValue("");
  });

  it("sends the traffic through the card that holds the default route", async () => {
    // A LAN card that is a port of a host bridge: the bridge has the address
    // and the gateway, and libvirt's NAT bridge must not win.
    fakeBackend({
      nics: [
        testNetworkInterface({ name: "enp2s0" }),
        testNetworkInterface({
          gateway: "192.168.1.1",
          ipv4: ["192.168.1.66/24"],
          name: "br-enp2s0",
        }),
        testNetworkInterface({ ipv4: ["192.168.122.1/24"], name: "virbr0" }),
      ],
    });
    const view = render(<CreateInterfaceButton interfaces={[]} />);

    await view.user.click(
      screen.getByRole("button", { name: "Create New Interface" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Create New Interface",
    });

    expect(
      await within(dialog).findByText(
        "Port 51820 · 10.10.20.0/24 · through br-enp2s0",
      ),
    ).toBeInTheDocument();
  });

  it("opens Advanced by itself when no NIC is available", async () => {
    fakeBackend({ nics: [] });
    const view = render(<CreateInterfaceButton interfaces={[]} />);

    await view.user.click(
      screen.getByRole("button", { name: "Create New Interface" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Create New Interface",
    });

    await waitFor(() =>
      expect(
        within(dialog).getByRole("button", { name: "Advanced" }),
      ).toHaveAttribute("aria-expanded", "true"),
    );
    expect(
      within(dialog).getByRole("combobox", { name: "NIC" }),
    ).toHaveTextContent("No NICs Available");
  });

  it("sends the request it always sent and opens the first device's QR code", async () => {
    const backend = fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);

    await view.user.click(
      within(dialog).getByRole("button", { name: "Create Interface" }),
    );

    await waitFor(() => expect(backend.creates).toHaveLength(1));
    expect(backend.creates[0]).toEqual({
      name: "wg0",
      addresses: "10.10.20.0/24",
      listenPort: "51820",
      egressNic: "eth0",
      host: undefined,
      dns: "",
      mtu: "0",
      numPeers: "1",
    });
    await waitFor(() => expect(navigate).toHaveBeenCalledTimes(1));
    expect(backend.peerReads).toEqual(["wg0"]);
    expect(lastSearch()).toEqual({ iface: "wg0", qr: "wg0-peer1" });
  });

  it("sends the optional host and MTU with the create request", async () => {
    const backend = fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);

    await view.user.click(
      within(dialog).getByRole("button", { name: "Advanced" }),
    );
    await view.user.type(
      within(dialog).getByLabelText("Endpoint host (optional)"),
      "vpn.example.org",
    );
    await view.user.type(
      within(dialog).getByLabelText("MTU (optional)"),
      "1400",
    );
    await view.user.click(
      within(dialog).getByRole("button", { name: "Create Interface" }),
    );

    await waitFor(() => expect(backend.creates).toHaveLength(1));
    expect(backend.creates[0]).toMatchObject({
      name: "wg0",
      addresses: "10.10.20.0/24",
      listenPort: "51820",
      egressNic: "eth0",
      host: "vpn.example.org",
      mtu: "1400",
    });
  });

  it("selects an interface made with no devices and opens no QR code", async () => {
    const backend = fakeBackend();
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);

    const devices = within(dialog).getByLabelText("Devices");
    await view.user.clear(devices);
    await view.user.type(devices, "0");
    await view.user.click(
      within(dialog).getByRole("button", { name: "Create Interface" }),
    );

    await waitFor(() => expect(navigate).toHaveBeenCalledTimes(1));
    expect(backend.creates[0]).toMatchObject({ numPeers: "0" });
    expect(backend.peerReads).toEqual([]);
    expect(lastSearch()).toEqual({ iface: "wg0" });
  });

  it("still selects the interface when its peers cannot be read", async () => {
    const backend = fakeBackend({
      peers: () => Promise.reject(new Error("no peers")),
    });
    const view = render(<CreateInterfaceButton interfaces={[]} />);
    const dialog = await openDialog(view);

    await view.user.click(
      within(dialog).getByRole("button", { name: "Create Interface" }),
    );

    await waitFor(() => expect(navigate).toHaveBeenCalledTimes(1));
    expect(backend.creates).toHaveLength(1);
    expect(lastSearch()).toEqual({ iface: "wg0" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
