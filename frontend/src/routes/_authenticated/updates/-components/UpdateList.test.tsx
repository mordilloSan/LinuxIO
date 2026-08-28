import { describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";

const query = vi.fn().mockResolvedValue(undefined);

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    // Neutralize only the changelog-detail fetch; config slice reads keep
    // going through the real useQuery against the seeded test cache.
    useQuery: (options: { queryKey?: readonly unknown[] }) =>
      options.queryKey?.[1] === "updates"
        ? { data: undefined, isError: false, isLoading: false }
        : actual.useQuery(options as Parameters<typeof actual.useQuery>[0]),
    useQueryClient: () => ({ query }),
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

vi.mock("@/components/cards/UpdateCard", () => ({
  default: ({
    isExpanded,
    onPrefetchChangelog,
    onToggleChangelog,
    update,
  }: {
    isExpanded: boolean;
    onPrefetchChangelog: () => void;
    onToggleChangelog: () => void;
    update: Update;
  }) => (
    <div>
      <button
        aria-label={`View Changelog ${update.package_id}`}
        onMouseEnter={onPrefetchChangelog}
        onClick={onToggleChangelog}
        type="button"
      />
      <span data-testid={`expanded-${update.package_id}`}>
        {String(isExpanded)}
      </span>
    </div>
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
  it("prefetches a changelog when its action is hovered", async () => {
    const { user } = render(
      <UpdateList onUpdateClick={vi.fn()} updates={[update("alpha")]} />,
    );

    await user.hover(
      screen.getByRole("button", { name: "View Changelog alpha" }),
    );

    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({
        meta: { silent: true },
        queryKey: [
          "linuxio",
          "updates",
          "get_update_detail",
          { packageId: "alpha" },
        ],
        retry: false,
        staleTime: 300_000,
      }),
    );
  });

  it("keeps changelog expansion with its package when updates reorder", async () => {
    const { rerender, user } = render(
      <UpdateList
        onUpdateClick={vi.fn()}
        updates={[update("alpha"), update("beta")]}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "View Changelog beta" }),
    );
    expect(screen.getByTestId("expanded-beta")).toHaveTextContent("true");
    expect(screen.getByTestId("expanded-alpha")).toHaveTextContent("false");

    rerender(
      <UpdateList
        onUpdateClick={vi.fn()}
        updates={[update("beta"), update("alpha")]}
      />,
    );

    expect(screen.getByTestId("expanded-beta")).toHaveTextContent("true");
    expect(screen.getByTestId("expanded-alpha")).toHaveTextContent("false");
  });
});
