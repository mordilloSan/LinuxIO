import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { pending, status, setQueryData } = vi.hoisted(() => ({
  pending: {
    "power.start": false,
    "power.disable": false,
    "power.set_profile": false,
  } as Record<string, boolean>,
  setQueryData: vi.fn(),
  status: {
    active_profile: "balanced",
    backend: "tuned",
    install_command: "apt install tuned",
    package_name: "tuned",
    power_profiles_daemon_active: false,
    profiles: [{ name: "balanced", active: true, recommended: true }],
    recommended_profile: "balanced",
    tuned_activatable: true,
    tuned_active: false,
    tuned_available: true,
    tuned_startable: true,
    tuned_unit_available: true,
    tuned_unit_file_state: "enabled",
  },
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: status, isPending: false, error: null }),
  useQueryClient: () => ({ setQueryData }),
}));
vi.mock("@/api", () => ({
  linuxio: {
    power: {
      get_status: { queryKey: ["power"] },
      start: { route: "power.start" },
      disable: { route: "power.disable" },
      set_profile: { route: "power.set_profile" },
    },
  },
  useCallMutation: (endpoint: { route: string }) => ({
    isPending: pending[endpoint.route],
    mutate: vi.fn(),
  }),
}));
vi.mock("@/components/ui/AppActionIconButton", () => ({
  default: (props: {
    ariaLabel: string;
    disabled?: boolean;
    loading?: boolean;
    onClick: () => void;
  }) => (
    <button
      aria-label={props.ariaLabel}
      disabled={props.disabled}
      onClick={props.onClick}
    >
      {props.loading ? <span role="progressbar" /> : null}
    </button>
  ),
}));
vi.mock("@/components/cards/FrostedCard", () => ({
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("@/components/loaders/ComponentLoader", () => ({
  default: () => <span />,
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
vi.mock("@/components/ui/AppSelect", () => ({ default: () => <select /> }));
vi.mock("@/components/ui/AppTypography", () => ({
  default: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

import PowerSettingsSection from "./PowerSettingsSection";

describe("PowerSettingsSection TuneD action", () => {
  beforeEach(() => {
    pending["power.start"] = false;
    pending["power.disable"] = false;
    pending["power.set_profile"] = false;
    status.tuned_active = false;
  });

  it("shows start progress and disables conflicting actions", () => {
    pending["power.start"] = true;
    render(<PowerSettingsSection />);
    expect(
      screen.getByRole("button", { name: "Starting TuneD" }),
    ).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });

  it("shows disable progress when TuneD is active", () => {
    status.tuned_active = true;
    pending["power.disable"] = true;
    render(<PowerSettingsSection />);
    expect(
      screen.getByRole("button", { name: "Disabling TuneD" }),
    ).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });

  it("does not present profile progress as TuneD progress", () => {
    pending["power.set_profile"] = true;
    render(<PowerSettingsSection />);
    expect(screen.getByRole("button", { name: "Start TuneD" })).toBeDisabled();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });
});
