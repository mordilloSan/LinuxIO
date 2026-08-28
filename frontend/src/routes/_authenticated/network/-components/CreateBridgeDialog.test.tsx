import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import CreateBridgeDialog from "./CreateBridgeDialog";

interface BridgeCreateResult {
  backend: string;
  member: string;
  name: string;
}

interface BridgeCreateRequest {
  member: string;
  name: string;
}

const mocks = vi.hoisted(() => ({
  createBridge: vi.fn(),
  onClose: vi.fn(),
  options: {
    candidates: [] as Array<{
      backend?: string;
      eligible: boolean;
      mac: string;
      name: string;
      reasons?: string[];
    }>,
    warnings: [] as string[],
  },
  useCallMutation: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({
      data: mocks.options,
      isError: false,
      isPending: false,
    }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      network: {
        ...actual.linuxio.network,
        create_bridge: { route: "network.create_bridge" },
        get_bridge_options: { route: "network.get_bridge_options" },
      },
    },
    useCallMutation: mocks.useCallMutation,
  };
});

mocks.useCallMutation.mockImplementation(
  (
    _endpoint: unknown,
    config?: {
      success?: (
        result: BridgeCreateResult,
        request: BridgeCreateRequest,
      ) => void;
    },
  ) => ({
    isPending: false,
    mutate: (request: BridgeCreateRequest) => {
      mocks.createBridge(request);
      config?.success?.(
        {
          backend: "systemd-networkd",
          member: request.member,
          name: request.name,
        },
        request,
      );
    },
  }),
);

describe("CreateBridgeDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.options = { candidates: [], warnings: [] };
  });

  it("shows refusal reasons and blocks creation when no NIC is eligible", async () => {
    mocks.options = {
      candidates: [
        {
          backend: "NetworkManager",
          eligible: false,
          mac: "02:00:00:00:00:01",
          name: "enp2s0",
          reasons: ["interface already carries a host IP"],
        },
      ],
      warnings: ["Docker firewall rules may affect bridged traffic"],
    };

    render(<CreateBridgeDialog onClose={mocks.onClose} open />);

    await waitFor(() =>
      expect(screen.getByText("No eligible spare NICs")).toBeVisible(),
    );
    expect(screen.getByRole("listitem")).toHaveTextContent(
      "enp2s0: interface already carries a host IP",
    );
    expect(
      screen.getByText("Docker firewall rules may affect bridged traffic"),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Create bridge" }),
    ).toBeDisabled();
  });

  it("submits the capped default name and closes after success", async () => {
    mocks.options = {
      candidates: [
        {
          eligible: true,
          mac: "02:00:00:00:00:02",
          name: "enp1234567890",
        },
      ],
      warnings: [],
    };

    const { user } = render(
      <CreateBridgeDialog onClose={mocks.onClose} open />,
    );

    await waitFor(() =>
      expect(screen.getByDisplayValue("br-enp123456789")).toBeVisible(),
    );
    expect(
      screen.queryByText("Stage 2a safety summary"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Create bridge" }));

    expect(mocks.createBridge).toHaveBeenCalledWith({
      member: "enp1234567890",
      name: "br-enp123456789",
    });
    expect(mocks.onClose).toHaveBeenCalledTimes(1);
  });
});
