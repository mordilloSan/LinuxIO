import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api";
import { act, render, screen, waitFor } from "@/test/render";

import BridgeHandoffDialog from "./BridgeHandoffDialog";

type HandoffRequest = {
  consoleAcknowledged: boolean;
  member: string;
  name: string;
  operationId: string;
};

type MutationConfig = {
  error?: string | ((error: LinuxIOError, request: HandoffRequest) => void);
  success?: (result: { operationId: string }, request: HandoffRequest) => void;
};

const mocks = vi.hoisted(() => ({
  interfaces: [
    {
      dns: [],
      gateway: "192.0.2.1",
      ipv4: ["192.0.2.20/24"],
      mac: "02:00:00:00:00:01",
      name: "enp2s0",
      type: "ethernet",
    },
  ],
  mutationConfigs: {} as Record<string, MutationConfig>,
  mutationRequests: [] as HandoffRequest[],
  onClose: vi.fn(),
  options: {
    candidates: [
      {
        eligible: false,
        handoffEligible: false,
        handoffReasons: ["interface is already a bridge"],
        mac: "02:00:00:00:00:01",
        name: "enp2s0",
      },
    ],
    warnings: [],
  },
  queryOptions: [] as Array<Record<string, unknown>>,
  status: undefined as
    | { error?: string; message?: string; operationId: string; state: string }
    | undefined,
  statusError: null as LinuxIOError | null,
  statusFailureCount: 0,
  useCallMutation: vi.fn(),
  useQuery: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: Record<string, unknown>) => {
      mocks.queryOptions.push(options);
      const queryKey = options.queryKey as unknown[] | undefined;
      const route = queryKey?.[2];
      if (route === "get_network_info") {
        return { data: mocks.interfaces, isError: false, isPending: false };
      }
      if (route === "get_bridge_options") {
        return { data: mocks.options, isError: false, isPending: false };
      }
      return {
        data: mocks.status,
        error: mocks.statusError,
        failureCount: mocks.statusFailureCount,
        isError: Boolean(mocks.statusError),
        isPending: false,
        state: { data: mocks.status },
      };
    },
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
        get_bridge_handoff: Object.assign(
          (request: { operationId: string }) => ({
            queryKey: ["linuxio", "network", "get_bridge_handoff", request],
          }),
          { route: "network.get_bridge_handoff" },
        ),
        get_bridge_options: {
          queryKey: ["linuxio", "network", "get_bridge_options"],
          route: "network.get_bridge_options",
        },
        get_network_info: {
          queryKey: ["linuxio", "network", "get_network_info"],
          route: "network.get_network_info",
        },
        start_bridge_handoff: {
          route: "network.start_bridge_handoff",
        },
      },
    },
    useCallMutation: mocks.useCallMutation,
    useStreamMux: () => ({ isOpen: true, status: "open" }),
  };
});

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({ error: vi.fn(), success: vi.fn() }),
}));

mocks.useCallMutation.mockImplementation(
  (endpoint: { route: string }, config: MutationConfig = {}) => {
    mocks.mutationConfigs[endpoint.route] = config;
    return {
      isPending: false,
      mutate: (request: HandoffRequest) => {
        mocks.mutationRequests.push(request);
      },
    };
  },
);

const makeEligibleOptions = () => ({
  candidates: [
    {
      eligible: false,
      handoffEligible: false,
      handoffReasons: ["interface is already a bridge"],
      mac: "02:00:00:00:00:01",
      name: "br-existing",
    },
    {
      eligible: false,
      handoffEligible: true,
      handoffReasons: [],
      mac: "02:00:00:00:00:02",
      name: "enp3s0",
    },
  ],
  warnings: [],
});

const openDialog = () =>
  render(<BridgeHandoffDialog onClose={mocks.onClose} open />);

const acknowledgeConsole = async () => {
  const { user } = openDialog();
  await user.click(screen.getByRole("checkbox"));
  return user;
};

describe("BridgeHandoffDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.interfaces = [
      {
        dns: [],
        gateway: "192.0.2.1",
        ipv4: ["192.0.2.20/24"],
        mac: "02:00:00:00:00:01",
        name: "enp2s0",
        type: "ethernet",
      },
    ];
    mocks.mutationConfigs = {};
    mocks.mutationRequests = [];
    mocks.options = {
      candidates: [
        {
          eligible: false,
          handoffEligible: false,
          handoffReasons: ["interface is already a bridge"],
          mac: "02:00:00:00:00:01",
          name: "enp2s0",
        },
      ],
      warnings: [],
    };
    mocks.queryOptions = [];
    mocks.status = undefined;
    mocks.statusError = null;
    mocks.statusFailureCount = 0;
  });

  it("only offers handoffEligible candidates and requires console acknowledgement", async () => {
    mocks.options = makeEligibleOptions();
    const { user } = openDialog();
    const start = screen.getByRole("button", { name: "Move IP to bridge" });

    await user.click(
      screen.getByRole("combobox", { name: "Management interface" }),
    );
    expect(screen.queryByRole("option", { name: /br-existing/ })).toBeNull();
    expect(screen.getByRole("option", { name: /enp3s0/ })).toBeVisible();
    await user.keyboard("{Escape}");
    expect(start).toBeDisabled();

    await user.click(screen.getByRole("checkbox"));
    expect(start).toBeEnabled();
    await user.click(start);

    expect(mocks.mutationRequests).toHaveLength(1);
    expect(mocks.mutationRequests[0]).toMatchObject({
      consoleAcknowledged: true,
      member: "enp3s0",
      name: "br-enp3s0",
    });
  });

  it("clears the operation after a non-connection start error so the user can retry", async () => {
    mocks.options = makeEligibleOptions();
    const user = await acknowledgeConsole();
    const start = screen.getByRole("button", { name: "Move IP to bridge" });
    await user.click(start);

    const request = mocks.mutationRequests[0];
    const error = new LinuxIOError("bridge validation failed", "validation");
    const config = mocks.mutationConfigs["network.start_bridge_handoff"];
    await act(async () => {
      if (typeof config.error === "function") config.error(error, request);
    });

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Move IP to bridge" }),
      ).toBeEnabled();
    });
    await user.click(screen.getByRole("button", { name: "Move IP to bridge" }));
    expect(mocks.mutationRequests).toHaveLength(2);
  });

  it.each(["outcome_unknown", "timeout"])(
    "keeps the client operation ID for status polling after %s",
    async (code) => {
      mocks.options = makeEligibleOptions();
      const user = await acknowledgeConsole();
      await user.click(
        screen.getByRole("button", { name: "Move IP to bridge" }),
      );

      const request = mocks.mutationRequests[0];
      const config = mocks.mutationConfigs["network.start_bridge_handoff"];
      await act(async () => {
        if (typeof config.error === "function") {
          config.error(
            new LinuxIOError("request outcome is ambiguous", code),
            request,
          );
        }
      });

      expect(
        screen.getByText("Waiting for the host to reconnect…"),
      ).toBeVisible();
      const statusQuery = mocks.queryOptions.find(
        (options) =>
          (options.queryKey as unknown[] | undefined)?.[2] ===
            "get_bridge_handoff" &&
          (
            (options.queryKey as unknown[] | undefined)?.[3] as
              | { operationId?: string }
              | undefined
          )?.operationId === request.operationId,
      );
      expect(statusQuery).toBeDefined();
      if (!statusQuery) {
        throw new Error("expected bridge handoff status query");
      }
      expect(statusQuery.enabled).toBe(true);
      expect((statusQuery.queryKey as unknown[])[3]).toEqual({
        operationId: request.operationId,
      });
      expect(statusQuery.meta).toEqual({ silent: true });
    },
  );

  it("does not poll status before Start has settled", async () => {
    mocks.options = makeEligibleOptions();
    const { rerender, user } = openDialog();
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Move IP to bridge" }));

    const request = mocks.mutationRequests[0];
    const statusForRequest = () =>
      mocks.queryOptions.find(
        (options) =>
          (options.queryKey as unknown[] | undefined)?.[2] ===
            "get_bridge_handoff" &&
          (
            (options.queryKey as unknown[] | undefined)?.[3] as
              | { operationId?: string }
              | undefined
          )?.operationId === request.operationId,
      );
    expect(statusForRequest()).toBeUndefined();

    const config = mocks.mutationConfigs["network.start_bridge_handoff"];
    await act(async () => {
      config.success?.({ operationId: request.operationId }, request);
    });
    rerender(<BridgeHandoffDialog onClose={mocks.onClose} open />);

    expect(statusForRequest()).toMatchObject({ enabled: true });
  });

  it("offers a safe reset after repeated 404s resolve an ambiguous Start", async () => {
    mocks.options = makeEligibleOptions();
    const { rerender, user } = openDialog();
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Move IP to bridge" }));

    const request = mocks.mutationRequests[0];
    const config = mocks.mutationConfigs["network.start_bridge_handoff"];
    await act(async () => {
      if (typeof config.error === "function") {
        config.error(
          new LinuxIOError("request outcome is ambiguous", "outcome_unknown"),
          request,
        );
      }
    });
    mocks.statusError = new LinuxIOError("network handoff not found", 404);
    mocks.statusFailureCount = 2;
    rerender(<BridgeHandoffDialog onClose={mocks.onClose} open />);

    expect(screen.getByText(/No handoff record was created/i)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Revert" })).toBeNull();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Reset and retry" }));
    expect(
      screen.getByRole("button", { name: "Move IP to bridge" }),
    ).toBeDisabled();
  });

  it("presents a status timeout as temporary while polling continues", async () => {
    mocks.options = makeEligibleOptions();
    const { rerender, user } = openDialog();
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Move IP to bridge" }));

    const request = mocks.mutationRequests[0];
    const config = mocks.mutationConfigs["network.start_bridge_handoff"];
    await act(async () => {
      if (typeof config.error === "function") {
        config.error(
          new LinuxIOError("request outcome is ambiguous", "outcome_unknown"),
          request,
        );
      }
    });
    mocks.statusError = new LinuxIOError("Request timeout", "timeout");
    mocks.statusFailureCount = 1;
    rerender(<BridgeHandoffDialog onClose={mocks.onClose} open />);

    expect(
      screen.getByText(/Status is temporarily unavailable/i),
    ).toBeVisible();
    expect(screen.queryByText("Request timeout")).toBeNull();
    const statusQuery = mocks.queryOptions.find(
      (options) =>
        (options.queryKey as unknown[] | undefined)?.[2] ===
          "get_bridge_handoff" && options.enabled === true,
    );
    expect(statusQuery?.refetchInterval).toEqual(expect.any(Function));
    if (typeof statusQuery?.refetchInterval !== "function") {
      throw new Error("expected the status query to keep polling");
    }
    expect(
      (
        statusQuery.refetchInterval as (query: {
          state: { data?: { state: string } };
        }) => number | false
      )({ state: { data: undefined } }),
    ).toBe(1000);
  });

  it("stops polling when the server cannot determine a confirmation outcome", async () => {
    mocks.options = makeEligibleOptions();
    const { rerender, user } = openDialog();
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Move IP to bridge" }));

    const request = mocks.mutationRequests[0];
    await act(async () => {
      mocks.mutationConfigs["network.start_bridge_handoff"].success?.(
        { operationId: request.operationId },
        request,
      );
    });
    mocks.status = {
      error: "confirmation outcome is unknown; inspect the host bridge",
      operationId: request.operationId,
      state: "unknown",
    };
    rerender(<BridgeHandoffDialog onClose={mocks.onClose} open />);

    expect(screen.getByText("Inspect the host bridge state")).toBeVisible();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Revert" })).toBeNull();
    const statusQuery = mocks.queryOptions.find(
      (options) =>
        (options.queryKey as unknown[] | undefined)?.[2] ===
          "get_bridge_handoff" && options.enabled === true,
    );
    expect(statusQuery).toBeDefined();
    if (!statusQuery) throw new Error("expected bridge handoff status query");
    expect(
      (
        statusQuery.refetchInterval as (query: {
          state: { data?: { state: string } };
        }) => number | false
      )({ state: { data: mocks.status } }),
    ).toBe(false);
  });
});
