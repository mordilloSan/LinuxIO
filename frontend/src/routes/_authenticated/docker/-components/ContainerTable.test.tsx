import { describe, expect, it, vi } from "vitest";

import type { ContainerInfo } from "@/api";
import * as core from "@/api/linuxio-core";
import { act, render, screen, waitFor, within } from "@/test/render";

import ContainerTable from "./ContainerTable";

const media = vi.hoisted(() => ({ compact: false }));

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

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ alt }: { alt: string }) => <span>{alt}</span>,
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
): ContainerInfo {
  return {
    Created: 1,
    Id: id,
    Image: "alpine:latest",
    Names: [`/${name}`],
    State: state,
    Status: state === "running" ? "Up 1 minute" : "Exited",
  };
}

function renderTable(containers: ContainerInfo[]) {
  return render(
    <ContainerTable
      autoUpdateDisabled={false}
      autoUpdatePendingNames={new Set()}
      autoUpdateSelectedNames={new Set()}
      containers={containers}
      onToggleAutoUpdate={vi.fn()}
    />,
  );
}

function rowNamed(name: string) {
  return screen.getByRole("row", { name: new RegExp(name, "i") });
}

describe("ContainerTable mutation feedback", () => {
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
