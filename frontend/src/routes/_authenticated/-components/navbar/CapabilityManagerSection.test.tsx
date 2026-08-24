import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { StrictMode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  CapabilitiesResponse,
  CapabilityRequest,
  InstallCapabilityProgress,
  InstallCapabilityResult,
  TaskProgress,
  TaskSnapshot,
} from "@/api";
import type { CapabilityInstallOutputLine } from "@/components/dialog/CapabilityInstallDialog";

interface TaskStreamConfig {
  error?: (error: Error, variables: CapabilityRequest) => void;
  onProgress?: (
    progress: TaskProgress<InstallCapabilityProgress>,
    task: TaskSnapshot,
    variables: CapabilityRequest,
  ) => void;
  success?: (
    result: InstallCapabilityResult,
    variables: CapabilityRequest,
  ) => void;
}

interface RecoveryConfig {
  onRecover?: (task: TaskSnapshot) => void;
  scanKey?: string | null;
  type?: string;
}

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  packageKitAvailable: true,
  recoveryConfig: null as RecoveryConfig | null,
  refreshCapabilities: vi.fn(),
  taskConfig: null as TaskStreamConfig | null,
  watch: vi.fn(),
}));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({
    refreshCapabilities: mocks.refreshCapabilities,
  }),
}));

vi.mock("@/hooks/useCapabilities", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/hooks/useCapabilities")>();
  const { emptyCapabilityState } =
    await vi.importActual<typeof import("@/api/capabilities")>(
      "@/api/capabilities",
    );
  return {
    ...actual,
    useCapabilityState: () => ({
      ...emptyCapabilityState,
      dockerAvailable: false,
      lmSensorsAvailable: false,
      packageKitAvailable: mocks.packageKitAvailable,
    }),
  };
});

vi.mock("@/hooks/backgroundTasks/useActiveTaskRecovery", () => ({
  useActiveTaskRecovery: (config: RecoveryConfig) => {
    mocks.recoveryConfig = config;
    return { isScanning: false, status: "missed" };
  },
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      system: {
        ...actual.linuxio.system,
        install_capability: {
          ...actual.linuxio.system.install_capability,
          useTaskStreamAction: (config: TaskStreamConfig) => {
            mocks.taskConfig = config;
            return { mutate: mocks.mutate, watch: mocks.watch };
          },
        },
      },
    },
    useStreamMux: () => ({ isOpen: true, status: "open" }),
  };
});

vi.mock("@/components/dialog/CapabilityInstallDialog", () => ({
  default: ({
    capabilityLabel,
    error,
    message,
    onClose,
    open,
    output,
    outputHistoryIncomplete,
    percentage,
    running,
    success,
    warning,
  }: {
    capabilityLabel: string;
    error: string | null;
    message: string;
    onClose: () => void;
    open: boolean;
    output: CapabilityInstallOutputLine[];
    outputHistoryIncomplete: boolean;
    percentage: number | null;
    running: boolean;
    success: boolean;
    warning: string | null;
  }) =>
    open ? (
      <div role="dialog">
        <span>Installing {capabilityLabel}</span>
        <span>{message}</span>
        {percentage === null ? null : <span>{percentage}%</span>}
        {output.map((record, index) => (
          <span key={index}>
            {record.stream}:{record.text}
          </span>
        ))}
        {outputHistoryIncomplete ? <span>Retained output only</span> : null}
        {success ? <span>Install succeeded</span> : null}
        {warning ? <span>{warning}</span> : null}
        {error ? <span>{error}</span> : null}
        <button onClick={onClose} type="button">
          {running ? "Run in background" : "Close"}
        </button>
      </div>
    ) : null,
}));

vi.mock("@/components/cards/FrostedCard", () => ({
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("@/components/ui/AppAlert", () => ({
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AppAlertTitle: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/components/ui/AppButton", () => ({
  default: ({
    children,
    startIcon,
    ...props
  }: ButtonHTMLAttributes<HTMLButtonElement> & { startIcon?: ReactNode }) => (
    <button {...props}>
      {startIcon}
      {children}
    </button>
  ),
}));
vi.mock("@/components/ui/AppChip", () => ({
  default: ({ label }: { label: string }) => <span>{label}</span>,
}));
vi.mock("@/components/ui/AppIconButton", () => ({
  default: ({
    children,
    color: _color,
    size: _size,
    ...props
  }: ButtonHTMLAttributes<HTMLButtonElement> & {
    color?: string;
    size?: string;
  }) => <button {...props}>{children}</button>,
}));
vi.mock("@/components/ui/AppTooltip", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/ui/AppTypography", () => ({
  default: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));
vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

import CapabilityManagerSection from "./CapabilityManagerSection";

const request = { capability: "lm_sensors" };
const task = {
  created_at: "2026-08-24T00:00:00Z",
  id: "task-1",
  metadata: { capability: "lm_sensors" },
  state: "running",
  type: "system.install_capability",
  updated_at: "2026-08-24T00:00:01Z",
} satisfies TaskSnapshot;

describe("CapabilityManagerSection", () => {
  beforeEach(() => {
    mocks.mutate.mockReset();
    mocks.packageKitAvailable = true;
    mocks.recoveryConfig = null;
    mocks.refreshCapabilities.mockReset();
    mocks.refreshCapabilities.mockResolvedValue({});
    mocks.taskConfig = null;
    mocks.watch.mockReset();
  });

  it("keeps async refresh updates alive after StrictMode effect replay", async () => {
    let resolveRefresh!: (value: CapabilitiesResponse) => void;
    mocks.refreshCapabilities.mockReturnValue(
      new Promise<CapabilitiesResponse>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    render(
      <StrictMode>
        <CapabilityManagerSection />
      </StrictMode>,
    );

    expect(mocks.refreshCapabilities).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveRefresh({ docker_available: true } as CapabilitiesResponse);
    });

    await waitFor(() => {
      expect(screen.getByText("Docker is reachable.")).toBeInTheDocument();
      expect(
        screen.queryByText("Saved sign-in snapshot"),
      ).not.toBeInTheDocument();
    });
  });

  it("opens the dialog before starting and renders streamed output", async () => {
    mocks.mutate.mockImplementation(() => {
      expect(screen.getByRole("dialog")).toBeInTheDocument();
    });
    render(<CapabilityManagerSection />);

    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));

    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledWith(request));
    expect(screen.getByText("Installing lm-sensors")).toBeInTheDocument();

    act(() =>
      mocks.taskConfig?.onProgress?.(
        {
          detail: {
            message: "Running sensors-detect",
            output: { stream: "stderr", text: "Driver warning\n" },
            percentage: 86,
            stage: "post_install",
          },
          message: "Running sensors-detect",
          percentage: 86,
          phase: "post_install",
        },
        task,
        request,
      ),
    );

    expect(
      screen.getAllByText("Running sensors-detect").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/stderr:Driver warning/)).toBeInTheDocument();
    expect(screen.getAllByText("86%").length).toBeGreaterThan(0);
  });

  it("closes without canceling and can reopen an active install", async () => {
    render(<CapabilityManagerSection />);
    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledOnce());

    fireEvent.click(screen.getByRole("button", { name: "Run in background" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.mutate).toHaveBeenCalledOnce();

    fireEvent.click(
      screen.getByRole("button", { name: "View lm-sensors installation" }),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(mocks.mutate).toHaveBeenCalledOnce();
  });

  it("bounds retained output and marks a truncated history", async () => {
    render(<CapabilityManagerSection />);
    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledOnce());

    act(() => {
      for (let index = 0; index <= 500; index += 1) {
        mocks.taskConfig?.onProgress?.(
          {
            detail: {
              message: "Probing sensors",
              output: { stream: "stdout", text: `line ${index}` },
              percentage: 86,
              stage: "post_install",
            },
            message: "Probing sensors",
            percentage: 86,
            phase: "post_install",
          },
          task,
          request,
        );
      }
    });

    expect(screen.queryByText("stdout:line 0")).not.toBeInTheDocument();
    expect(screen.getByText("stdout:line 500")).toBeInTheDocument();
    expect(screen.getByText("Retained output only")).toBeInTheDocument();
  });

  it("shows terminal success, warning, and failure results", async () => {
    const { unmount } = render(<CapabilityManagerSection />);
    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledOnce());

    act(() => mocks.taskConfig?.success?.({ available: true }, request));
    expect(screen.getByText("Install succeeded")).toBeInTheDocument();
    unmount();

    render(<CapabilityManagerSection />);
    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));
    act(() =>
      mocks.taskConfig?.success?.(
        { available: false, error: "sensors is unavailable" },
        request,
      ),
    );
    expect(
      screen.getAllByText("sensors is unavailable").length,
    ).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "Install lm-sensors" }));
    act(() =>
      mocks.taskConfig?.error?.(new Error("sensors-detect failed"), request),
    );
    expect(screen.getByText("sensors-detect failed")).toBeInTheDocument();
  });

  it("recovers an active task with an honest retained-output marker", async () => {
    render(<CapabilityManagerSection />);

    act(() => mocks.recoveryConfig?.onRecover?.(task));

    await waitFor(() =>
      expect(mocks.watch).toHaveBeenCalledWith(task, request),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Retained output only")).toBeInTheDocument();
  });

  it("keeps PackageKit-backed installs blocked when PackageKit is absent", () => {
    mocks.packageKitAvailable = false;
    render(<CapabilityManagerSection />);

    expect(
      screen.getByRole("button", { name: "Install lm-sensors" }),
    ).toBeDisabled();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
