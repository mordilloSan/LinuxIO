import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  DockerContainerUpdateProgress,
  DockerContainerUpdateResult,
  TaskProgress,
} from "@/api";
import AppButton from "@/components/ui/AppButton";
import { act, render, screen } from "@/test/render";

import {
  DockerUpdateOperationProvider,
  useDockerUpdateOperation,
} from "./DockerUpdateOperationProvider";

interface TaskStreamConfig {
  onProgress?: (
    progress: TaskProgress<DockerContainerUpdateProgress>,
  ) => void;
  success?: (result: DockerContainerUpdateResult) => void;
}

const mocks = vi.hoisted(() => ({
  config: null as TaskStreamConfig | null,
  mutate: vi.fn(),
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
          useTaskStreamAction: (config: TaskStreamConfig) => {
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

vi.mock("@/hooks/backgroundTasks/useActiveTaskRecovery", () => ({
  useActiveTaskRecovery: () => ({ isScanning: false, status: "missed" }),
}));

const StartUpdate = () => {
  const { startUpdate } = useDockerUpdateOperation();
  return (
    <AppButton onClick={() => startUpdate("container-123", "homepage")}>
      Update homepage
    </AppButton>
  );
};

describe("DockerUpdateOperationProvider", () => {
  beforeEach(() => {
    mocks.config = null;
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
      runId: expect.any(String),
    });
    expect(screen.getByText("Updating homepage")).toBeInTheDocument();

    act(() =>
      mocks.config?.onProgress?.({
        detail: { phase: "pulling", message: "Pulling image homepage:latest" },
        message: "Pulling image homepage:latest",
        phase: "pulling",
      }),
    );
    expect(
      screen.getAllByText(/Pulling image homepage:latest/),
    ).toHaveLength(2);
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
});
