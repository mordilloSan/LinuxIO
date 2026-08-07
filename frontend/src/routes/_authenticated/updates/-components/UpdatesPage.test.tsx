import { describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";

const mocks = vi.hoisted(() => ({
  refreshCache: vi.fn(),
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
          useAction: () => ({
            isPending: false,
            mutate: mocks.refreshCache,
          }),
        },
      },
    },
  };
});

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ status: "available" }),
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({ success: vi.fn() }),
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
vi.mock("./UpdateSettingsDialog", () => ({ default: () => null }));

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
});
