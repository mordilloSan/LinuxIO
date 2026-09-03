import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor, within } from "@/test/render";

import NetworkList from "./NetworkList";
import VolumeList from "./VolumeList";

const mocks = vi.hoisted(() => ({
  containers: [] as unknown[],
  data: [] as unknown[],
  mutate: vi.fn(),
  mutateAsync: vi.fn().mockResolvedValue(undefined),
  navigate: vi.fn(),
  navigateApp: vi.fn(),
  search: {} as { network?: string; volume?: string },
  startDownload: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: (options: { queryKey?: unknown[] }) => ({
      data: options.queryKey?.includes("list_containers")
        ? mocks.containers
        : mocks.data,
    }),
  };
});

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => mocks.navigate,
      useSearch: () => mocks.search,
    }),
    useNavigate: () => mocks.navigateApp,
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({
      isPending: false,
      mutate: mocks.mutate,
      mutateAsync: mocks.mutateAsync,
    }),
  };
});

vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({ startDownload: mocks.startDownload }),
}));

describe("Docker network and volume focused details", () => {
  beforeEach(() => {
    mocks.data = [];
    mocks.containers = [];
    mocks.search = {};
    mocks.mutate.mockReset();
    mocks.mutateAsync.mockReset();
    mocks.mutateAsync.mockResolvedValue(undefined);
    mocks.navigate.mockReset();
    mocks.navigateApp.mockReset();
    mocks.startDownload.mockReset();
    mocks.startDownload.mockResolvedValue(undefined);
    mocks.navigate.mockImplementation(
      ({ search }: { search: (current: typeof mocks.search) => object }) => {
        mocks.search = search(mocks.search);
        return Promise.resolve();
      },
    );
  });

  it("opens a network on one click with its delete action in the focused card", async () => {
    mocks.data = [
      {
        Attachable: false,
        ConfigOnly: false,
        Containers: {},
        Driver: "bridge",
        EnableIPv4: true,
        EnableIPv6: false,
        Id: "network-id",
        Ingress: false,
        Internal: false,
        IPAM: { Config: [], Driver: "default" },
        Labels: {},
        Name: "example-network",
        Options: {},
        Protected: false,
        Scope: "local",
      },
    ];
    const { rerender, user } = render(<NetworkList viewMode="card" />);

    await user.click(
      screen.getByRole("button", {
        name: "Open network example-network details",
      }),
    );
    rerender(<NetworkList viewMode="card" />);

    expect(
      screen.getByRole("button", { name: "Delete network example-network" }),
    ).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("Search networks…")).toBeNull();
  });

  it("opens a volume on one click with its delete action in the focused card", async () => {
    mocks.data = [
      {
        Driver: "local",
        Labels: {},
        Mountpoint: "/var/lib/docker/volumes/example-volume/_data",
        MountpointAccessible: true,
        Name: "example-volume",
        Options: {},
        Scope: "local",
      },
    ];
    const { rerender, user } = render(<VolumeList viewMode="card" />);

    await user.click(
      screen.getByRole("button", {
        name: "Open volume example-volume details",
      }),
    );
    rerender(<VolumeList viewMode="card" />);

    await user.click(
      screen.getByRole("button", {
        name: "Actions for volume example-volume",
      }),
    );
    expect(
      screen.getByRole("menuitem", { name: "Delete volume" }),
    ).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("Search volumes…")).toBeNull();
  });

  it("creates a volume with its driver and optional labels", async () => {
    let openCreate: (() => void) | undefined;
    const { user } = render(
      <VolumeList
        onMountCreateHandler={(handler) => {
          openCreate = handler;
        }}
      />,
    );
    await waitFor(() => expect(openCreate).toBeDefined());
    act(() => openCreate?.());

    const dialog = screen.getByRole("dialog", { name: "Create volume" });
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Name/ }),
      "cache",
    );
    await user.click(within(dialog).getByRole("combobox", { name: "Driver" }));
    await user.click(screen.getByRole("option", { name: "Custom…" }));
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Custom driver/ }),
      "custom-driver",
    );
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Labels/ }),
      "purpose=build\nteam=platform",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Create volume" }),
    );

    expect(mocks.mutate).toHaveBeenCalledWith({
      name: "cache",
      driver: "custom-driver",
      labels: { purpose: "build", team: "platform" },
    });
  });

  it("browses and backs up an accessible volume with running-writer warning", async () => {
    mocks.search = { volume: "data" };
    mocks.data = [
      {
        Containers: [
          { Id: "one", Name: "writer", State: "running" },
          { Id: "two", Name: "reader", State: "exited" },
        ],
        Driver: "local",
        Labels: {},
        Mountpoint: "/var/lib/docker/volumes/data/_data",
        MountpointAccessible: true,
        Name: "data",
        Options: {},
        Scope: "local",
      },
    ];
    const { user } = render(<VolumeList viewMode="card" />);

    await user.click(
      screen.getByRole("button", { name: "Browse volume data in Navigator" }),
    );
    expect(mocks.navigateApp).toHaveBeenCalledWith({
      to: "/filebrowser/$",
      params: { _splat: "var/lib/docker/volumes/data/_data" },
    });

    await user.click(
      screen.getByRole("button", { name: "Actions for volume data" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "Download backup" }));
    const dialog = screen.getByRole("dialog", {
      name: "Download backup of data?",
    });
    expect(dialog).toHaveTextContent("writer (running)");
    expect(dialog).toHaveTextContent("inconsistent data");
    await user.click(
      within(dialog).getByRole("button", { name: "Download backup" }),
    );
    expect(mocks.startDownload).toHaveBeenCalledWith([
      "/var/lib/docker/volumes/data/_data/",
    ]);
  });

  it("creates a network with addressing and advanced driver options", async () => {
    let openCreate: (() => void) | undefined;
    const { user } = render(
      <NetworkList
        onMountCreateHandler={(handler) => {
          openCreate = handler;
        }}
      />,
    );
    await waitFor(() => expect(openCreate).toBeDefined());
    act(() => openCreate?.());

    const dialog = screen.getByRole("dialog", { name: "Create network" });
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Name/ }),
      "app-net",
    );
    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "Internal network (no external connectivity)",
      }),
    );
    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "Allow manual container attachment",
      }),
    );
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Subnet/ }),
      "172.20.0.0/16",
    );
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Gateway/ }),
      "172.20.0.1",
    );
    await user.click(within(dialog).getByRole("button", { name: "Advanced" }));
    await user.type(
      within(dialog).getByRole("textbox", { name: /^Driver options/ }),
      "com.docker.network.bridge.name=br-app",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Create network" }),
    );

    expect(mocks.mutate).toHaveBeenCalledWith({
      name: "app-net",
      driver: "bridge",
      internal: true,
      attachable: true,
      enableIpv6: false,
      subnet: "172.20.0.0/16",
      gateway: "172.20.0.1",
      options: { "com.docker.network.bridge.name": "br-app" },
    });
  });

  it("connects an unattached container and confirms disconnection", async () => {
    mocks.search = { network: "network-id" };
    mocks.data = [
      {
        Attachable: true,
        ConfigOnly: false,
        Containers: {
          attached: { EndpointID: "endpoint", Name: "attached" },
        },
        Driver: "bridge",
        EnableIPv4: true,
        EnableIPv6: false,
        Id: "network-id",
        Ingress: false,
        Internal: false,
        IPAM: { Config: [], Driver: "default" },
        Labels: {},
        Name: "app-net",
        Options: {},
        Protected: false,
        Scope: "local",
      },
    ];
    mocks.containers = [
      { Id: "attached", Names: ["/attached"], State: "running", Status: "Up" },
      {
        Id: "candidate",
        Names: ["/candidate"],
        State: "exited",
        Status: "Exited",
      },
    ];
    const { user } = render(<NetworkList viewMode="card" />);

    await user.click(screen.getByRole("button", { name: "Connect container" }));
    const connectDialog = screen.getByRole("dialog", {
      name: "Connect container to app-net",
    });
    await user.type(
      within(connectDialog).getByRole("textbox", { name: /^Aliases/ }),
      "api, web",
    );
    await user.click(
      within(connectDialog).getByRole("button", {
        name: "Connect container",
      }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      networkId: "network-id",
      containerId: "candidate",
      aliases: ["api", "web"],
    });
    await user.click(
      within(connectDialog).getByRole("button", { name: "Cancel" }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Connect container to app-net" }),
      ).not.toBeInTheDocument(),
    );

    await user.click(
      screen.getByRole("button", { name: "Disconnect container attached" }),
    );
    const disconnectDialog = screen.getByRole("dialog", {
      name: "Disconnect attached from app-net?",
    });
    await user.click(
      within(disconnectDialog).getByRole("button", {
        name: "Disconnect container",
      }),
    );
    expect(mocks.mutate).toHaveBeenLastCalledWith({
      networkId: "network-id",
      containerId: "attached",
    });
  });

  it("does not offer deletion for Docker default networks", () => {
    mocks.search = { network: "bridge-id" };
    mocks.data = [
      {
        Attachable: false,
        ConfigOnly: false,
        Containers: {},
        Driver: "bridge",
        EnableIPv4: true,
        EnableIPv6: false,
        Id: "bridge-id",
        Ingress: false,
        Internal: false,
        Name: "bridge",
        Protected: true,
        Scope: "local",
      },
    ];
    render(<NetworkList viewMode="card" />);
    expect(
      screen.queryByRole("button", { name: "Delete network bridge" }),
    ).toBeNull();
    expect(
      screen.getByText("Docker default networks cannot be deleted."),
    ).toBeInTheDocument();
  });
});
