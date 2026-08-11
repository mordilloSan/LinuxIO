import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { act, fireEvent, render, screen, waitFor } from "@/test/render";

import DockerInfo from "./Docker";

const queryData = vi.hoisted(() => ({
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
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: (options: {
      select?: (containers: typeof queryData.containers) => unknown;
    }) => ({
      data: options.select
        ? options.select(queryData.containers)
        : queryData.containers,
    }),
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
});
