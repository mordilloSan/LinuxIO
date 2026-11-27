import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  query: vi.fn().mockResolvedValue(undefined),
  search: {} as { package?: string },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => ({ query: mocks.query }),
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
  const getUpdateDetail = Object.assign(
    (request: { packageId: string }) => ({
      queryKey: ["linuxio", "updates", "get_update_detail", request],
    }),
    { route: "updates.get_update_detail" },
  );
  return {
    ...actual,
    CACHE_TTL_MS: { ...actual.CACHE_TTL_MS, FIVE_MINUTES: 300_000 },
    linuxio: {
      ...actual.linuxio,
      updates: {
        ...actual.linuxio.updates,
        get_update_detail: getUpdateDetail,
      },
    },
  };
});

vi.mock("./UpdateChangelog", () => ({
  default: ({ packageId }: { packageId: string }) => (
    <div data-testid="changelog">{packageId}</div>
  ),
}));

const { default: UpdateList } = await import("./UpdateList");
const { render, screen } = await import("@/test/render");

const update = (package_id: string): Update => ({
  changelog: "",
  cve: [],
  info_enum: 11,
  issued: "",
  package_id,
  restart: 0,
  state: 0,
  summary: package_id,
  version: "1.0",
});

describe("UpdateList", () => {
  beforeEach(() => {
    mocks.search = {};
    mocks.navigate.mockReset();
    mocks.navigate.mockImplementation(
      ({ search }: { search: (current: typeof mocks.search) => object }) => {
        mocks.search = search(mocks.search);
        return Promise.resolve();
      },
    );
    mocks.query.mockClear();
  });

  it("prefetches a changelog when its card is hovered", async () => {
    const { user } = render(
      <UpdateList onUpdateClick={vi.fn()} updates={[update("alpha")]} />,
    );

    await user.hover(screen.getByText("alpha", { selector: ".app-typo" }));

    expect(mocks.query).toHaveBeenCalledWith(
      expect.objectContaining({
        meta: { silent: true },
        queryKey: [
          "linuxio",
          "updates",
          "get_update_detail",
          { packageId: "alpha" },
        ],
        staleTime: 300_000,
      }),
    );
  });

  it("focuses an update in the address when its card is clicked", async () => {
    const { user } = render(
      <UpdateList
        onUpdateClick={vi.fn()}
        updates={[update("alpha"), update("beta")]}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Open update beta details" }),
    );

    expect(mocks.search).toEqual({ package: "beta" });
  });

  it("shows the focused update's changelog beside its card with the Update action", async () => {
    mocks.search = { package: "beta" };
    const onUpdateClick = vi.fn();
    const { user } = render(
      <UpdateList
        onUpdateClick={onUpdateClick}
        updates={[update("alpha"), update("beta")]}
      />,
    );

    expect(screen.getByTestId("changelog")).toHaveTextContent("beta");
    expect(
      screen.queryByRole("button", { name: "Open update alpha details" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Update" }));
    expect(onUpdateClick).toHaveBeenCalledWith("beta");

    await user.click(
      screen.getByRole("button", { name: "Close update details" }),
    );
    expect(mocks.search).toEqual({ package: undefined });
  });

  it("closes the details when the focused update leaves the list", () => {
    mocks.search = { package: "gone" };
    render(<UpdateList onUpdateClick={vi.fn()} updates={[update("alpha")]} />);

    expect(mocks.search).toEqual({ package: undefined });
    expect(screen.queryByTestId("changelog")).not.toBeInTheDocument();
  });
});
