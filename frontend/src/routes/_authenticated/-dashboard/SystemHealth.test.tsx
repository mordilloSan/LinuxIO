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
}

const {
  dismissFailedLoginAlert,
  healthState,
  openNavigate,
  pendingState,
  updatesState,
} = vi.hoisted(() => ({
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
  } as MockHealthState,
  openNavigate: vi.fn(),
  pendingState: {
    "system.dismiss_failed_login_alert": false,
    "system.dismiss_unclean_shutdown": false,
  } as Record<string, boolean>,
  updatesState: {
    isError: false,
    list: ["pkg"] as string[] | undefined,
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: healthState,
    }),
    useQuery: (options: {
      queryKey: string[];
      select?: (data: string[]) => unknown;
    }) =>
      options.queryKey[1] === "updates"
        ? {
            data: updatesState.list && options.select?.(updatesState.list),
            isError: updatesState.isError,
          }
        : { data: [], isLoading: false, isError: false },
  };
});

vi.mock("@iconify/react", () => ({
  Icon: ({ color, icon }: { color?: string; icon: string }) => (
    <span data-color={color} data-icon={icon} />
  ),
}));

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ isEnabled: true }),
}));

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
      updates: {
        get_updates_basic: {
          queryKey: ["linuxio", "updates", "get_updates_basic"],
        },
      },
    },
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ user: { name: "miguel" } }),
}));
vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
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
    });
    Object.assign(updatesState, { isError: false, list: ["pkg"] });
    delete healthState.lastLogin;
    delete healthState.uncleanShutdownBootId;
  });

  const shieldColor = () =>
    screen
      .getByRole("button", { name: "View system status" })
      .querySelector("[data-icon]")
      ?.getAttribute("data-color");

  it("keeps the shield neutral while updates are still checking", () => {
    healthState.failedLoginAlert = undefined;
    updatesState.list = undefined;
    render(<SystemHealth />);
    expect(screen.getByText("Checking for updates")).toBeInTheDocument();
    expect(screen.queryByText(/up to date/i)).not.toBeInTheDocument();
    expect(shieldColor()).toBe("var(--app-palette-text-secondary)");
  });

  it("says update status is unavailable instead of up to date", () => {
    healthState.failedLoginAlert = undefined;
    updatesState.list = undefined;
    updatesState.isError = true;
    render(<SystemHealth />);
    expect(screen.getByText("Update status unavailable")).toBeInTheDocument();
    expect(screen.queryByText(/up to date/i)).not.toBeInTheDocument();
  });

  it("turns the shield green once updates report none pending", () => {
    healthState.failedLoginAlert = undefined;
    updatesState.list = [];
    render(<SystemHealth />);
    expect(screen.getByText("System is up to date")).toBeInTheDocument();
    expect(shieldColor()).toBe("var(--app-palette-success-dark)");
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
