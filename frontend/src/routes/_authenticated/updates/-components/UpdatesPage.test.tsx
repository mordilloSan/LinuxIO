import { describe, expect, it, vi } from "vitest";

import type { RefreshCacheResult, Update } from "@/api";

const mocks = vi.hoisted(() => ({
  refreshCache: vi.fn(),
  // What the page's refresh mutation was given, and what it last answered.
  refreshConfig: undefined as
    | { success: (result: RefreshCacheResult) => Promise<void> }
    | undefined,
  refreshResult: undefined as RefreshCacheResult | undefined,
  toastSuccess: vi.fn(),
  toastWarning: vi.fn(),
  updateAll: vi.fn(),
  updates: [] as Update[],
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: mocks.updates,
      refetch: vi.fn(),
    }),
  };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      updates: {
        ...actual.linuxio.updates,
        get_updates_basic: {
          queryOptions: () => ({}),
        },
        refresh_cache: {
          useMutation: (config: typeof mocks.refreshConfig) => {
            mocks.refreshConfig = config;
            return {
              data: mocks.refreshResult,
              isPending: false,
              mutate: mocks.refreshCache,
            };
          },
        },
      },
    },
  };
});

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ status: "available" }),
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({
    success: mocks.toastSuccess,
    warning: mocks.toastWarning,
  }),
}));

vi.mock("./PackageUpdateController", () => ({
  usePackageUpdateController: () => ({
    cancelUpdate: vi.fn(),
    clearError: vi.fn(),
    error: null,
    eventLog: [],
    progress: 0,
    recoveryPending: false,
    status: null,
    updateAll: mocks.updateAll,
    updateOne: vi.fn(),
    updatingPackage: null,
  }),
}));

vi.mock("./UpdateStatus", () => ({ default: () => null }));

const { default: UpdatesPage } = await import("./UpdatesPage");
const { render, screen } = await import("@/test/render");

const update = (info_enum: number, package_id: string): Update => ({
  changelog: "",
  cve: [],
  info_enum,
  issued: "",
  package_id,
  restart: 0,
  state: 0,
  summary: package_id,
  version: "1.0",
});

describe("UpdatesPage", () => {
  it("counts and submits only currently actionable updates", async () => {
    mocks.updates = [update(11, "ready"), update(9, "later")];
    const { user } = render(<UpdatesPage />);

    expect(
      screen.getByRole("button", { name: "Update All (1)" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Update All (1)" }));
    expect(mocks.updateAll).toHaveBeenCalledWith(["ready"]);
  });

  it("does not show Update All when every update is deferred", () => {
    mocks.updates = [update(9, "later"), update(25, "also-later")];
    render(<UpdatesPage />);

    expect(
      screen.queryByRole("button", { name: /Update All/ }),
    ).not.toBeInTheDocument();
  });

  it("says when the last refresh could not read LinuxIO's repository", () => {
    mocks.updates = [];
    mocks.refreshResult = {
      own_repository_error:
        'Failed to download metadata for repository "linuxio"',
    };
    try {
      render(<UpdatesPage />);

      const alert = screen.getByRole("alert");
      expect(alert).toHaveTextContent(
        "LinuxIO's package repository could not be read",
      );
      expect(alert).toHaveTextContent(
        'The package manager reported: Failed to download metadata for repository "linuxio"',
      );
    } finally {
      mocks.refreshResult = undefined;
    }
  });

  it("says nothing of LinuxIO's repository when the refresh read it", () => {
    mocks.updates = [];
    mocks.refreshResult = {};
    try {
      render(<UpdatesPage />);
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    } finally {
      mocks.refreshResult = undefined;
    }
  });

  it("warns in the refresh's toast instead of reporting success", async () => {
    mocks.updates = [];
    render(<UpdatesPage />);

    await mocks.refreshConfig?.success({ own_repository_error: "unreadable" });
    expect(mocks.toastWarning).toHaveBeenCalledWith(
      "Update sources refreshed, but LinuxIO's repository could not be read",
    );
    expect(mocks.toastSuccess).not.toHaveBeenCalled();

    await mocks.refreshConfig?.success({});
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Update sources refreshed");
  });

  it("keeps automatic update settings in global Settings", () => {
    mocks.updates = [];
    render(<UpdatesPage />);

    expect(
      screen.queryByRole("button", { name: "Open update settings" }),
    ).not.toBeInTheDocument();
  });
});
