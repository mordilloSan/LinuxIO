import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import NetworkList from "./NetworkList";
import VolumeList from "./VolumeList";

const mocks = vi.hoisted(() => ({
  data: [] as unknown[],
  navigate: vi.fn(),
  search: {} as { network?: string; volume?: string },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({ data: mocks.data }),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => mocks.navigate,
      useSearch: () => mocks.search,
    }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ isPending: false, mutateAsync: vi.fn() }),
  };
});

describe("Docker network and volume focused details", () => {
  beforeEach(() => {
    mocks.data = [];
    mocks.search = {};
    mocks.navigate.mockReset();
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
  });

  it("opens a volume on one click with its delete action in the focused card", async () => {
    mocks.data = [
      {
        Driver: "local",
        Labels: {},
        Mountpoint: "/var/lib/docker/volumes/example-volume/_data",
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

    expect(
      screen.getByRole("button", { name: "Delete volume example-volume" }),
    ).toBeInTheDocument();
  });
});
