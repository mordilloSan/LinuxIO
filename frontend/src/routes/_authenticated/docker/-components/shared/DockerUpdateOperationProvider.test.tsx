import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  DockerContainerUpdateProgress,
  DockerContainerUpdateResult,
  TaskProgress,
  TaskSnapshot,
} from "@/api";
import AppButton from "@/components/ui/AppButton";
import { act, render, screen } from "@/test/render";

import {
  DockerUpdateOperationProvider,
  useDockerUpdateOperation,
} from "./DockerUpdateOperationProvider";

interface MutationConfig {
  onProgress?: (progress: TaskProgress<DockerContainerUpdateProgress>) => void;
  success?: (result: DockerContainerUpdateResult) => void;
}

const mocks = vi.hoisted(() => ({
  config: null as MutationConfig | null,
  mutate: vi.fn(),
  onRecover: null as ((task: TaskSnapshot) => void) | null,
  watch: vi.fn(),
}));

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        update_container: {
          ...actual.linuxio.docker.update_container,
          useMutation: (config: MutationConfig) => {
            mocks.config = config;
            return {
              isPending: false,
              mutate: mocks.mutate,
              watch: mocks.watch,
            };
          },
        },
      },
    },
    useStreamMux: () => ({ isOpen: true, status: "open" }),
  };
});

vi.mock("@/tasks/useActiveTaskRecovery", () => ({
  useActiveTaskRecovery: (options: {
    onRecover: (task: TaskSnapshot) => void;
  }) => {
    mocks.onRecover = options.onRecover;
    return { isScanning: false, status: "missed" };
  },
}));

const StartUpdate = () => {
  const { startUpdate } = useDockerUpdateOperation();
  return (
    <AppButton onClick={() => startUpdate("container-123", "homepage")}>
      Update homepage
    </AppButton>
  );
};

const StartRebuild = () => {
  const { startRebuild } = useDockerUpdateOperation();
  return (
    <AppButton onClick={() => startRebuild("container-123", "homepage")}>
      Rebuild homepage
    </AppButton>
  );
};

const progress = (phase: string, message: string) =>
  mocks.config?.onProgress?.({
    detail: { phase, message },
    message,
    phase,
  });

const CanUpdate = () => {
  const { canUpdate } = useDockerUpdateOperation();
  return <span>{canUpdate ? "can update" : "cannot update"}</span>;
};

describe("DockerUpdateOperationProvider", () => {
  beforeEach(() => {
    mocks.config = null;
    mocks.onRecover = null;
    mocks.mutate.mockReset();
    mocks.watch.mockReset();
  });

  it("opens an update dialog and presents progress and the typed result", async () => {
    const { user } = render(
      <DockerUpdateOperationProvider>
        <StartUpdate />
      </DockerUpdateOperationProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Update homepage" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      containerId: "container-123",
    });
    expect(screen.getByText("Updating homepage")).toBeInTheDocument();

    act(() =>
      mocks.config?.onProgress?.({
        detail: { phase: "pulling", message: "Pulling image homepage:latest" },
        message: "Pulling image homepage:latest",
        phase: "pulling",
      }),
    );
    expect(screen.getAllByText(/Pulling image homepage:latest/)).toHaveLength(
      2,
    );
    const details = screen.getByRole("button", { name: "Show details" });
    expect(details).toHaveAttribute("aria-expanded", "false");
    await user.click(details);
    expect(
      screen.getByRole("button", { name: "Hide details" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(document.getElementById("docker-update-details")).toHaveAttribute(
      "aria-hidden",
      "false",
    );

    act(() =>
      mocks.config?.success?.({
        containerId: "container-456",
        containerName: "homepage",
        image: "homepage:latest",
        newImageId: "sha256:bbbbbbbbbbbb2222",
        previousImageId: "sha256:aaaaaaaaaaaa1111",
        updated: true,
      }),
    );
    expect(screen.getByText("Container update completed")).toBeInTheDocument();
    expect(screen.getByText("aaaaaaaaaaaa")).toBeInTheDocument();
    expect(screen.getByText("bbbbbbbbbbbb")).toBeInTheDocument();
  });

  it("rebuilds and keeps the build output in one bounded block", async () => {
    const { user } = render(
      <DockerUpdateOperationProvider>
        <StartRebuild />
      </DockerUpdateOperationProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Rebuild homepage" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      containerId: "container-123",
      rebuild: true,
    });
    expect(screen.getByText("Rebuilding homepage")).toBeInTheDocument();

    act(() => {
      progress("building", "Building the image of Compose service web");
      for (let line = 1; line <= 305; line++) {
        progress("build_output", `#${line} step`);
      }
    });
    // One row for the phase; its lines are in the build output alone.
    expect(
      screen.getAllByText("Building the image of Compose service web"),
    ).toHaveLength(1);
    const output = screen.getByRole("region", { name: "Build output" });
    expect(output.children).toHaveLength(300);
    expect(output.firstElementChild).toHaveTextContent("#6 step");
    expect(output.lastElementChild).toHaveTextContent("#305 step");

    act(() =>
      mocks.config?.success?.({
        containerId: "container-123",
        containerName: "homepage",
        image: "homepage-web",
        newImageId: "sha256:aaaaaaaaaaaa1111",
        previousImageId: "sha256:aaaaaaaaaaaa1111",
        updated: false,
      }),
    );
    expect(
      screen.getByText("The rebuilt image is the one already running"),
    ).toBeInTheDocument();
  });

  it("shows a rebuild adopted after a reload from its build output alone", () => {
    render(
      <DockerUpdateOperationProvider>
        <StartUpdate />
      </DockerUpdateOperationProvider>,
    );

    act(() =>
      mocks.onRecover?.({
        id: "job-2",
        metadata: { identity: ["container-123"] },
      } as TaskSnapshot),
    );
    // The replay of a long build holds its last lines, not its first rows.
    act(() => {
      progress("build_output", "#9 RUN apk add curl");
      progress("build_output", "#9 DONE 2.1s");
    });

    expect(screen.getByText(/^Rebuilding/)).toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Build output" }),
    ).toHaveTextContent("#9 DONE 2.1s");
  });

  it("adopts a recovered update by its job id", () => {
    render(
      <DockerUpdateOperationProvider>
        <StartUpdate />
      </DockerUpdateOperationProvider>,
    );

    act(() =>
      mocks.onRecover?.({
        id: "job-1",
        metadata: { identity: ["container-123"] },
      } as TaskSnapshot),
    );

    expect(mocks.watch).toHaveBeenCalledExactlyOnceWith("job-1", {
      containerId: "container-123",
    });
  });

  it("reports canUpdate only for a privileged session", () => {
    const { unmount } = render(
      <DockerUpdateOperationProvider>
        <CanUpdate />
      </DockerUpdateOperationProvider>,
    );
    expect(screen.getByText("cannot update")).toBeInTheDocument();
    unmount();

    render(
      <DockerUpdateOperationProvider>
        <CanUpdate />
      </DockerUpdateOperationProvider>,
      { auth: { privileged: true } },
    );
    expect(screen.getByText("can update")).toBeInTheDocument();
  });
});
