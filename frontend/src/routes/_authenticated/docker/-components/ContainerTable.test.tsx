import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ContainerInfo } from "@/api";
import * as core from "@/api/linuxio-core";
import type { AppDataTableDndOptions } from "@/components/tables/AppDataTable";
import { act, render, screen, waitFor, within } from "@/test/render";

import type { ContainerTableRow } from "./containerStacks";
import ContainerTable from "./ContainerTable";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const media = vi.hoisted(() => ({ compact: false }));

const updateCheckMocks = vi.hoisted(() => ({
  useCallMutation: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  updateCheckMocks.useCallMutation.mockImplementation(
    (
      endpoint: { route?: string },
      config: { success?: (result: unknown) => void },
    ) => {
      if (endpoint.route === "docker.check_container_update") {
        return {
          isPending: false,
          mutate: () =>
            config.success?.({
              checked: 1,
              errors: 1,
              uncheckable: 0,
              updates: 0,
            }),
        };
      }
      return actual.useCallMutation(endpoint as never, config);
    },
  );
  return {
    ...actual,
    useCallMutation: updateCheckMocks.useCallMutation,
  };
});

vi.mock("@/theme", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/theme")>();
  return {
    ...actual,
    useAppMediaQuery: () => media.compact,
  };
});

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

beforeEach(() => {
  vi.clearAllMocks();
});

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ alt, identifier }: { alt: string; identifier: string }) => (
    <span data-identifier={identifier} data-testid="docker-icon">
      {alt}
    </span>
  ),
}));

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function container(
  id: string,
  name: string,
  state: "exited" | "running",
  updateStatus: Partial<ContainerInfo> = {},
): ContainerInfo {
  return {
    Created: 1,
    Id: id,
    Image: "alpine:latest",
    Names: [`/${name}`],
    State: state,
    Status: state === "running" ? "Up 1 minute" : "Exited",
    ...updateStatus,
  };
}

function renderTable(containers: ContainerInfo[]) {
  return render(<ContainerTable containers={containers} />);
}

function rowNamed(name: string) {
  return screen.getByRole("row", { name: new RegExp(name, "i") });
}

function composeContainer(
  id: string,
  name: string,
  project: string,
  state: "exited" | "running" = "running",
) {
  return container(id, name, state, {
    Labels: { "com.docker.compose.project": project },
  });
}

// The collapse state lives with the page (ContainerList); this stands in for
// it so the toggle round-trips.
function StatefulStackTable({ containers }: { containers: ContainerInfo[] }) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  return (
    <ContainerTable
      collapsedStackIds={collapsed}
      containers={containers}
      onToggleStack={(project) =>
        setCollapsed((previous) => {
          const next = new Set(previous);
          if (!next.delete(project)) next.add(project);
          return next;
        })
      }
    />
  );
}

const reorderDnd: AppDataTableDndOptions<ContainerTableRow> = {
  contextProps: {} as never,
  editing: true,
  getItemId: (row) => row.id,
  handleAriaLabel: "Reorder container",
  itemIds: [],
};

describe("ContainerTable mutation feedback", () => {
  it("shows a warning and never claims up to date after a failed per-container scan", async () => {
    media.compact = false;
    const { user } = renderTable([container("failed-id", "failed", "running")]);

    await user.click(
      within(rowNamed("failed")).getByRole("button", {
        name: "Re-scan failed for updates",
      }),
    );

    const { toast } = await import("sonner");
    expect(toast.warning).toHaveBeenCalledWith(
      "Failed to check updates for failed: 1 error(s)",
      expect.anything(),
    );
    expect(toast.success).not.toHaveBeenCalledWith(
      "Container failed is up to date",
      expect.anything(),
    );
  });

  it("shows local-only images as uncheckable instead of up to date", () => {
    media.compact = false;
    renderTable([
      container("local-id", "local", "running", {
        updateCheckedAt: Date.now(),
        updateCheckReason: "local image has no repository digest",
        updateCheckState: "uncheckable",
      }),
    ]);

    const rescan = within(rowNamed("local")).getByRole("button", {
      name: "Re-scan local for updates",
    });
    expect(rescan).toHaveTextContent("Cannot check");
    expect(rescan).not.toHaveTextContent("Up to date");
  });

  it.each([
    ["Start", "start_container", "exited"],
    ["Stop", "stop_container", "running"],
    ["Restart", "restart_container", "running"],
    ["Remove", "remove_container", "exited"],
  ] as const)(
    "keeps %s pending on only the affected row",
    async (label, command, state) => {
      media.compact = false;
      const deferred = createDeferred<void>();
      const request = vi
        .spyOn(core, "request")
        .mockImplementation((_handler, calledCommand) =>
          calledCommand === command ? deferred.promise : Promise.resolve(),
        );
      const { user } = renderTable([
        container("alpha-id", "alpha", state),
        container("beta-id", "beta", state),
      ]);
      const alpha = within(rowNamed("alpha"));
      const beta = within(rowNamed("beta"));

      await user.click(alpha.getByRole("button", { name: label }));

      await waitFor(() => {
        expect(alpha.getByRole("button", { name: label })).toBeDisabled();
      });
      expect(
        within(alpha.getByRole("button", { name: label })).getByRole(
          "progressbar",
        ),
      ).toBeInTheDocument();
      expect(alpha.getByRole("button", { name: "Restart" })).toBeDisabled();
      expect(alpha.getByRole("button", { name: "Remove" })).toBeDisabled();
      expect(beta.getByRole("button", { name: label })).toBeEnabled();
      expect(
        within(beta.getByRole("button", { name: label })).queryByRole(
          "progressbar",
        ),
      ).not.toBeInTheDocument();
      expect(request).toHaveBeenCalledWith(
        "docker",
        command,
        { containerId: "alpha-id" },
        { retryPolicy: "none" },
      );

      await act(async () => {
        deferred.resolve(undefined);
        await deferred.promise;
      });
      await waitFor(() => {
        expect(alpha.getByRole("button", { name: label })).toBeEnabled();
      });
    },
  );

  it("groups a multi-container compose project under a header row, singletons not", () => {
    media.compact = false;
    renderTable([
      composeContainer("media-web-id", "media-web", "media"),
      composeContainer("media-db-id", "media-db", "media"),
      composeContainer("solo-id", "solo-app", "solo"),
    ]);

    const toggle = screen.getByRole("button", {
      name: "Collapse stack media",
    });
    const header = toggle.closest('[role="row"]') as HTMLElement;
    expect(
      within(header).getByText("2 containers · 2 running"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Collapse stack solo" }),
    ).not.toBeInTheDocument();
    // Members still render as ordinary rows below the header.
    expect(rowNamed("media-web")).toBeInTheDocument();
    expect(rowNamed("media-db")).toBeInTheDocument();
  });

  it("collapses a stack to its header row and expands it back", async () => {
    media.compact = false;
    const { user } = render(
      <StatefulStackTable
        containers={[
          composeContainer("media-web-id", "media-web", "media"),
          composeContainer("media-db-id", "media-db", "media", "exited"),
          composeContainer("solo-id", "solo-app", "solo"),
        ]}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Collapse stack media" }),
    );

    // Stack members remain mounted through the shared AppCollapse exit motion.
    expect(rowNamed("media-web").closest(".app-collapse")).toHaveClass(
      "app-collapse",
    );
    expect(rowNamed("media-db").closest(".app-collapse")).toHaveClass(
      "app-collapse",
    );
    // The summary keeps counting the hidden members; the loose row stays.
    expect(screen.getByText("2 containers · 1 running")).toBeInTheDocument();
    expect(rowNamed("solo-app")).toBeInTheDocument();
    const collapsedHeader = screen
      .getByRole("button", { name: "Expand stack media" })
      .closest('[role="row"]') as HTMLElement;
    expect(within(collapsedHeader).getByTestId("docker-icon")).toHaveAttribute(
      "data-identifier",
      "media",
    );

    await expect
      .poll(() => screen.queryAllByText("media-web").length, { timeout: 2000 })
      .toBe(0);
    await expect
      .poll(() => screen.queryAllByText("media-db").length, { timeout: 2000 })
      .toBe(0);

    await user.click(
      screen.getByRole("button", { name: "Expand stack media" }),
    );

    expect(rowNamed("media-web")).toBeInTheDocument();
    expect(rowNamed("media-db")).toBeInTheDocument();
  });

  it("keeps stack members inert while reordering their header", () => {
    media.compact = false;
    render(
      <ContainerTable
        containers={[
          composeContainer("media-web-id", "media-web", "media"),
          composeContainer("media-db-id", "media-db", "media"),
          container("solo-id", "solo-app", "running"),
        ]}
        dnd={reorderDnd}
      />,
    );

    const stackHeader = screen
      .getByRole("button", { name: "Collapse stack media" })
      .closest('[role="row"]') as HTMLElement;
    const mediaWeb = rowNamed("media-web");
    const mediaDb = rowNamed("media-db");
    const solo = rowNamed("solo-app");

    expect(stackHeader).toHaveClass("app-dt__row--reordering");
    expect(
      within(stackHeader).getByLabelText("Reorder container"),
    ).toBeInTheDocument();
    expect(mediaWeb).not.toHaveClass("app-dt__row--reordering");
    expect(mediaDb).not.toHaveClass("app-dt__row--reordering");
    expect(within(mediaWeb).queryByLabelText("Reorder container")).toBeNull();
    expect(within(mediaDb).queryByLabelText("Reorder container")).toBeNull();
    expect(solo).toHaveClass("app-dt__row--reordering");
    expect(
      within(solo).getByLabelText("Reorder container"),
    ).toBeInTheDocument();

    const stackDragBlock = stackHeader.closest(
      ".app-dt__sortable-group",
    ) as HTMLElement;
    expect(stackDragBlock).toContainElement(mediaWeb);
    expect(stackDragBlock).toContainElement(mediaDb);
    expect(stackDragBlock).not.toContainElement(solo);
  });

  it("keeps a compact row spinner after its action menu closes", async () => {
    media.compact = true;
    const deferred = createDeferred<void>();
    vi.spyOn(core, "request").mockImplementation((_handler, command) =>
      command === "start_container" ? deferred.promise : Promise.resolve(),
    );
    const { user } = renderTable([
      container("alpha-id", "alpha", "exited"),
      container("beta-id", "beta", "exited"),
    ]);

    await user.click(screen.getByRole("button", { name: "Actions for alpha" }));
    await user.click(screen.getByRole("menuitem", { name: "Start" }));

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    const alphaTrigger = screen.getByRole("button", {
      name: "Starting alpha",
    });
    const betaTrigger = screen.getByRole("button", {
      name: "Actions for beta",
    });
    await waitFor(() => expect(alphaTrigger).toBeDisabled());
    expect(within(alphaTrigger).getByRole("progressbar")).toBeInTheDocument();
    expect(betaTrigger).toBeEnabled();
    expect(within(betaTrigger).queryByRole("progressbar")).toBeNull();

    await act(async () => {
      deferred.resolve(undefined);
      await deferred.promise;
    });
    await waitFor(() => expect(alphaTrigger).toBeEnabled());
  });
});
