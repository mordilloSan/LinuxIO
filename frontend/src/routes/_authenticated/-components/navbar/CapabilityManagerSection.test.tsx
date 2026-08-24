import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { CapabilitiesResponse } from "@/api";

const { refreshCapabilities, useTaskStreamAction } = vi.hoisted(() => ({
  refreshCapabilities: vi.fn(),
  useTaskStreamAction: vi.fn(() => ({ mutateAsync: vi.fn() })),
}));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({
    refreshCapabilities,
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
      packageKitAvailable: false,
    }),
  };
});

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
          useTaskStreamAction,
        },
      },
    },
  };
});

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
  default: ({ children }: { children: ReactNode }) => (
    <button>{children}</button>
  ),
}));
vi.mock("@/components/ui/AppChip", () => ({
  default: ({ label }: { label: string }) => <span>{label}</span>,
}));
vi.mock("@/components/ui/AppIconButton", () => ({
  default: ({ children }: { children: ReactNode }) => (
    <button>{children}</button>
  ),
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

describe("CapabilityManagerSection", () => {
  it("keeps async refresh updates alive after StrictMode effect replay", async () => {
    let resolveRefresh!: (value: CapabilitiesResponse) => void;
    refreshCapabilities.mockReturnValue(
      new Promise<CapabilitiesResponse>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    render(
      <StrictMode>
        <CapabilityManagerSection />
      </StrictMode>,
    );

    expect(refreshCapabilities).toHaveBeenCalledTimes(2);

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
});
