import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { act, fireEvent, render, screen, waitFor } from "@/test/render";

import DockerInfo from "./Docker";

const queryData = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  return {
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    // A poll result reaches subscribed components the way a query observer
    // would; a parent re-render alone does not reach a hoisted child.
    notify: () => {
      for (const listener of listeners) listener();
    },
    containers: [
      {
        Created: 1,
        Id: "alpha-id",
        Image: "alpine:latest",
        Names: ["/alpha"],
        State: "exited",
        Status: "Exited",
      },
      {
        Created: 1,
        Id: "beta-id",
        Image: "alpine:latest",
        Names: ["/beta"],
        State: "running",
        Status: "Up 1 minute",
      },
    ],
  };
});

const allContainers = queryData.containers;

afterEach(() => {
  queryData.containers = allContainers;
});

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  const { useSyncExternalStore } = await import("react");
  return {
    ...actual,
    useSuspenseQuery: (options: {
      select?: (containers: typeof queryData.containers) => unknown;
    }) => {
      const containers = useSyncExternalStore(
        queryData.subscribe,
        () => queryData.containers,
      );
      return { data: options.select ? options.select(containers) : containers };
    },
  };
});

vi.mock("@/components/cards/DashboardCard", () => ({
  default: ({ stats2 }: { stats2: ReactNode }) => <section>{stats2}</section>,
}));

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ alt }: { alt: string }) => <span>{alt}</span>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
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

describe("dashboard Docker mutation feedback", () => {
  it("retains concurrent action targets after their menus close", async () => {
    const starting = createDeferred<void>();
    const restarting = createDeferred<void>();
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        const containerId = (request as { containerId?: string }).containerId;
        if (command === "start_container" && containerId === "alpha-id") {
          return starting.promise;
        }
        if (command === "restart_container" && containerId === "beta-id") {
          return restarting.promise;
        }
        return Promise.resolve();
      },
    );
    const { user } = render(<DockerInfo />);

    fireEvent.contextMenu(screen.getByRole("group", { name: "alpha" }));
    await user.click(screen.getByRole("menuitem", { name: "Start" }));

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("progressbar", { name: "Starting alpha" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: "alpha: Starting" }),
    ).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("group", { name: "beta" })).toHaveAttribute(
      "aria-busy",
      "false",
    );

    fireEvent.contextMenu(screen.getByRole("group", { name: "beta" }));
    await user.click(screen.getByRole("menuitem", { name: "Restart" }));

    expect(
      await screen.findByRole("progressbar", { name: "Restarting beta" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("progressbar", { name: "Starting alpha" }),
    ).toBeInTheDocument();

    fireEvent.contextMenu(
      screen.getByRole("group", { name: "alpha: Starting" }),
    );
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();

    await act(async () => {
      starting.resolve(undefined);
      await starting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("progressbar", { name: "Starting alpha" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("progressbar", { name: "Restarting beta" }),
    ).toBeInTheDocument();

    await act(async () => {
      restarting.resolve(undefined);
      await restarting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("progressbar", { name: "Restarting beta" }),
      ).not.toBeInTheDocument();
    });
  });

  it("drops an open menu when a poll removes the container it targets", () => {
    render(<DockerInfo />);

    fireEvent.contextMenu(screen.getByRole("group", { name: "alpha" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();

    queryData.containers = allContainers.filter((c) => c.Id !== "alpha-id");
    act(() => queryData.notify());

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("group", { name: "alpha" }),
    ).not.toBeInTheDocument();
  });
});
