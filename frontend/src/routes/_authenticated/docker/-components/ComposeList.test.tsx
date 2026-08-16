import { describe, expect, it, vi } from "vitest";

import type { ComposeProject, ContainerInfo } from "@/api";
import * as core from "@/api/linuxio-core";
import { act, render, screen, waitFor, within } from "@/test/render";

import ComposeList from "./ComposeList";

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

const alpha: ContainerInfo = {
  Created: 1,
  Id: "alpha-id",
  Image: "alpha:latest",
  Names: ["/alpha"],
  State: "exited",
  Status: "Exited",
  updateAvailable: true,
};

const beta: ContainerInfo = {
  Created: 1,
  Id: "beta-id",
  Image: "beta:latest",
  Names: ["/beta"],
  State: "running",
  Status: "Up 1 minute",
};

const project: ComposeProject = {
  config_files: ["/srv/demo/compose.yaml"],
  containers: [alpha, beta],
  name: "demo",
  services: {
    alpha: {
      container_count: 1,
      container_ids: [alpha.Id],
      image: alpha.Image,
      name: "alpha",
      ports: [],
      state: alpha.State,
      status: alpha.Status,
    },
    beta: {
      container_count: 1,
      container_ids: [beta.Id],
      image: beta.Image,
      name: "beta",
      ports: [],
      state: beta.State,
      status: beta.Status,
    },
  },
  status: "partial",
  update_available: true,
  working_dir: "/srv/demo",
};

const noopProject = (_project: ComposeProject) => {};
const noopName = (_name: string) => {};
const mocks = vi.hoisted(() => ({ startUpdate: vi.fn() }));

vi.mock("@/components/docker/DockerUpdateOperationProvider", () => ({
  useDockerUpdateOperation: () => ({
    isUpdating: () => false,
    startUpdate: mocks.startUpdate,
    updating: false,
  }),
}));

describe("ComposeList expanded-container mutation feedback", () => {
  it("keeps action progress scoped to each expanded container", async () => {
    const restarting = createDeferred<void>();
    mocks.startUpdate.mockReset();
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        const containerId = (request as { containerId?: string }).containerId;
        if (command === "restart_container" && containerId === beta.Id) {
          return restarting.promise;
        }
        return Promise.resolve();
      },
    );
    const { user } = render(
      <ComposeList
        onDelete={noopProject}
        onRestart={noopName}
        onStart={noopName}
        onStop={noopName}
        projects={[project]}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Expand row" }));
    const alphaActions = within(
      await screen.findByRole("group", { name: "Actions for alpha" }),
    );
    const betaActions = within(
      screen.getByRole("group", { name: "Actions for beta" }),
    );

    await user.click(
      alphaActions.getByRole("button", { name: "Update container" }),
    );
    expect(mocks.startUpdate).toHaveBeenCalledWith(alpha.Id, "alpha");
    expect(
      betaActions.getByRole("button", { name: "Restart container" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("group", { name: "Actions for beta" }),
    ).not.toHaveAttribute("aria-busy");

    await user.click(
      betaActions.getByRole("button", { name: "Restart container" }),
    );
    await waitFor(() => {
      expect(
        within(
          betaActions.getByRole("button", { name: "Restart container" }),
        ).getByRole("progressbar"),
      ).toBeInTheDocument();
    });

    expect(
      within(
        betaActions.getByRole("button", { name: "Restart container" }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();

    await act(async () => {
      restarting.resolve(undefined);
      await restarting.promise;
    });
    await waitFor(() => {
      expect(
        within(
          betaActions.getByRole("button", { name: "Restart container" }),
        ).queryByRole("progressbar"),
      ).not.toBeInTheDocument();
    });
  });
});
