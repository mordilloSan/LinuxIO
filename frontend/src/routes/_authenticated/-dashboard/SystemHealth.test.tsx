import type { AnchorHTMLAttributes, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

interface MockHealthState {
  failedLoginAlert?: {
    count: number;
    id: string;
    latestEvent: undefined;
  };
  failedServicesCount: number;
  lastLogin?: {
    source: string;
    terminal: string;
    time: string;
    username: string;
  };
  runningServicesCount: number;
  uncleanShutdown: boolean;
  uncleanShutdownBootId?: string;
  upToDate: boolean;
  updatesAvailable: number;
}

const { dismissFailedLoginAlert, healthState, openNavigate, pendingState } =
  vi.hoisted(() => ({
    dismissFailedLoginAlert: vi.fn(),
    healthState: {
      failedLoginAlert: {
        id: "alert-1",
        count: 2,
        latestEvent: undefined,
      },
      failedServicesCount: 0,
      runningServicesCount: 2,
      uncleanShutdown: false,
      upToDate: false,
      updatesAvailable: 1,
    } as MockHealthState,
    openNavigate: vi.fn(),
    pendingState: {
      "system.dismiss_failed_login_alert": false,
      "system.dismiss_unclean_shutdown": false,
    } as Record<string, boolean>,
  }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: healthState,
    }),
    useQuery: () => ({ data: [], isLoading: false, isError: false }),
  };
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    search,
    children,
    ...props
  }: Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & {
    children: ReactNode;
    search?: unknown;
    to: string;
  }) => (
    <a
      {...props}
      data-search={search ? JSON.stringify(search) : undefined}
      href={to}
    >
      {children}
    </a>
  ),
  useNavigate: () => openNavigate,
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    useCallMutation: (endpoint: { route?: string }) => ({
      mutate:
        endpoint.route === "system.dismiss_failed_login_alert"
          ? dismissFailedLoginAlert
          : vi.fn(),
      isPending: Boolean(endpoint.route && pendingState[endpoint.route]),
    }),
    linuxio: {
      system: {
        get_health_summary: {
          queryKey: ["linuxio", "system", "get_health_summary"],
        },
        list_failed_login_events: () => ({
          queryKey: ["linuxio", "system", "list_failed_login_events"],
        }),
        dismiss_unclean_shutdown: { route: "system.dismiss_unclean_shutdown" },
        dismiss_failed_login_alert: {
          route: "system.dismiss_failed_login_alert",
        },
      },
    },
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ user: { name: "miguel" } }),
}));
vi.mock("@/components/cards/DashboardCard", () => ({
  default: ({ stats, stats2 }: { stats: ReactNode; stats2: ReactNode }) => (
    <div>
      {stats}
      {stats2}
    </div>
  ),
}));

import SystemHealth from "./SystemHealth";

describe("SystemHealth interactions", () => {
  beforeEach(() => {
    dismissFailedLoginAlert.mockReset();
    openNavigate.mockReset();
    Object.assign(pendingState, {
      "system.dismiss_failed_login_alert": false,
      "system.dismiss_unclean_shutdown": false,
    });
    Object.assign(healthState, {
      failedLoginAlert: {
        id: "alert-1",
        count: 2,
        latestEvent: undefined,
      },
      failedServicesCount: 0,
      runningServicesCount: 2,
      uncleanShutdown: false,
      upToDate: false,
      updatesAvailable: 1,
    });
    delete healthState.lastLogin;
    delete healthState.uncleanShutdownBootId;
  });

  it("renders navigable rows as links", () => {
    render(<SystemHealth />);
    expect(
      screen.getByRole("link", { name: /services running/i }),
    ).toHaveAttribute("href", "/services");
    expect(
      screen.getByRole("link", { name: /update available/i }),
    ).toHaveAttribute("href", "/updates");
  });

  it("renders the last-login destination as an account link", () => {
    healthState.failedLoginAlert = undefined;
    healthState.lastLogin = {
      time: "Mon Jan 1 12:00:00",
      source: "ssh",
      terminal: "web",
      username: "miguel",
    };
    render(<SystemHealth />);
    const accountLink = screen.getByRole("link", { name: /last login/i });
    expect(accountLink).toHaveAttribute("href", "/accounts");
    expect(accountLink).toHaveAttribute(
      "data-search",
      expect.stringContaining('"user":"miguel"'),
    );
  });

  it("keeps failed-login dismissal separate from the row action", async () => {
    const { user } = render(<SystemHealth />);
    const row = screen.getByRole("button", { name: /failed login attempts/i });
    await user.click(
      screen.getByRole("button", { name: /dismiss failed login alert/i }),
    );
    expect(dismissFailedLoginAlert).toHaveBeenCalledWith({
      alertId: "alert-1",
    });
    expect(openNavigate).not.toHaveBeenCalled();
    expect(row).toBeInTheDocument();
    expect(screen.queryByText(/^Failed logins$/)).not.toBeInTheDocument();
  });

  it("shows failed-login dismissal progress without removing the row", () => {
    pendingState["system.dismiss_failed_login_alert"] = true;
    render(<SystemHealth />);
    expect(
      screen.getByRole("button", { name: "Dismissing failed login alert" }),
    ).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /failed login attempts/i }),
    ).toBeInTheDocument();
  });

  it("shows unclean-shutdown dismissal progress independently", () => {
    healthState.failedLoginAlert = undefined;
    healthState.uncleanShutdown = true;
    healthState.uncleanShutdownBootId = "boot-1";
    pendingState["system.dismiss_unclean_shutdown"] = true;
    render(<SystemHealth />);
    expect(
      screen.getByRole("button", { name: "Dismissing unclean shutdown alert" }),
    ).toBeDisabled();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });
});
