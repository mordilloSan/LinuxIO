import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ComposeTaskMessage, ComposeTaskResult } from "@/api";
import { act, render, screen } from "@/test/render";

import ComposeOperationDialog from "./ComposeOperationDialog";

interface TaskStreamConfig {
  onProgress?: (message: ComposeTaskMessage) => void;
  success?: (message: ComposeTaskResult) => void;
}

interface RecoveryConfig {
  onMiss?: () => void;
}

const mocks = vi.hoisted(() => ({
  watch: vi.fn(),
  taskConfig: null as TaskStreamConfig | null,
  mutate: vi.fn(),
  recoveryConfig: null as RecoveryConfig | null,
  toastError: vi.fn(),
  toastInfo: vi.fn(),
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
        compose: {
          ...actual.linuxio.docker.compose,
          useTaskStreamAction: (config: TaskStreamConfig) => {
            mocks.taskConfig = config;
            return { watch: mocks.watch, mutate: mocks.mutate };
          },
        },
      },
    },
    useStreamMux: () => ({ isOpen: true, status: "open" }),
  };
});

vi.mock("@/hooks/backgroundTasks/useActiveTaskRecovery", () => ({
  useActiveTaskRecovery: (config: RecoveryConfig) => {
    mocks.recoveryConfig = config;
    return { isScanning: false, status: "missed" };
  },
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({
    error: mocks.toastError,
    info: mocks.toastInfo,
  }),
}));

describe("ComposeOperationDialog", () => {
  beforeEach(() => {
    mocks.watch.mockReset();
    mocks.taskConfig = null;
    mocks.mutate.mockReset();
    mocks.recoveryConfig = null;
    mocks.toastError.mockReset();
    mocks.toastInfo.mockReset();
  });

  it("renders completion from the terminal result rather than progress", () => {
    render(
      <ComposeOperationDialog
        action="up"
        onClose={vi.fn()}
        open
        projectName="test-stack"
      />,
    );

    act(() => mocks.recoveryConfig?.onMiss?.());
    expect(mocks.mutate).toHaveBeenCalledWith({
      action: "up",
      composePath: undefined,
      projectName: "test-stack",
    });

    const terminal = {
      message: "operation completed successfully",
      type: "complete",
    } as const;

    act(() => mocks.taskConfig?.onProgress?.(terminal));
    expect(
      screen.queryByText("✓ operation completed successfully"),
    ).not.toBeInTheDocument();

    act(() => mocks.taskConfig?.success?.(terminal));
    expect(
      screen.getByText("✓ operation completed successfully"),
    ).toBeInTheDocument();
  });
});
