import { beforeEach, describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen, waitFor } from "@/test/render";

import SchedulesTab from "./SchedulesTab";

const mocks = vi.hoisted(() => ({
  mutations: [] as Array<{ route: string; request: unknown }>,
  navigate: vi.fn(),
}));

const status = {
  definition: {
    id: "backup",
    enabled: true,
    options: {
      name: "Backup",
      script: "echo old",
      user: "root",
      arguments: [],
      on_calendar: "daily",
      timezone: "UTC",
      persistent: false,
      timeout_seconds: 60,
      working_directory: "",
    },
  },
  unit_name: "linuxio-schedule-backup.service",
  active_state: "active",
  result: "success",
  invocation_id: "0123456789abcdef0123456789abcdef",
  exit_code: 0,
  exit_status: 0,
  next_run_at: null,
  last_run_at: null,
  can_stop: true,
  error: null,
};

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: { available: true, error: null, schedules: [] },
    }),
    useQuery: (options: { queryKey?: unknown[] }) => ({
      data: options.queryKey?.some((part) => String(part).includes("get"))
        ? status
        : [{ username: "root" }, { username: "alice" }],
    }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        directory_children: (request: {
          path: string;
          includeFiles: boolean;
        }) => ({
          queryKey: ["test", "directories", request],
          queryFn: async () => ({ folders: ["backups"], files: [] }),
        }),
      },
    },
    useCallMutation: (
      endpoint: { route: string },
      config?: { success?: () => void },
    ) => ({
      isPending: false,
      mutate: (request: unknown) => {
        mocks.mutations.push({ route: endpoint.route, request });
        config?.success?.();
      },
    }),
  };
});

const props = {
  listQueryOptions: {} as typeof import("@/api").linuxio.schedules.list,
  selectedQueryOptions: undefined,
  onSelectedChange: vi.fn(),
};

describe("SchedulesTab interactions", () => {
  beforeEach(() => {
    mocks.mutations = [];
    mocks.navigate.mockReset();
  });

  it("opens from null, selects root, and submits the exact create payload", async () => {
    const { user } = render(<SchedulesTab {...props} />);
    await user.click(screen.getByRole("button", { name: "Create task" }));
    await user.type(screen.getByLabelText("Name"), "nightly");
    await user.clear(screen.getByLabelText("Bash script"));
    await user.type(screen.getByLabelText("Bash script"), "echo ok");
    await user.click(screen.getByLabelText("Run as"));
    await user.click(screen.getByRole("option", { name: "root" }));
    await user.type(
      screen.getByLabelText("Arguments (one per line)"),
      "first{Enter}second value",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.create",
      request: {
        options: {
          name: "nightly",
          script: "echo ok",
          user: "root",
          arguments: ["first", "second value"],
          on_calendar: "*-*-* 03:00:00",
          timezone: "",
          persistent: false,
          timeout_seconds: 300,
          working_directory: "",
        },
      },
    });
  });

  it("saves a weekly time and the directory selected from the shared picker", async () => {
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await user.click(screen.getByRole("button", { name: "Edit" }));
    await user.click(screen.getByLabelText("Repeat"));
    await user.click(screen.getByRole("option", { name: "Weekly" }));
    await user.click(screen.getByLabelText("Day of the week"));
    await user.click(screen.getByRole("option", { name: "Saturday" }));
    // Native time controls accept a complete time value.
    fireEvent.change(screen.getByLabelText("Time"), {
      target: { value: "07:00" },
    });
    await user.click(
      screen.getByRole("button", { name: "Browse working directory" }),
    );
    await user.click(screen.getByRole("button", { name: "Expand /" }));
    await user.click(await screen.findByText("backups"));
    expect(screen.getByLabelText("Working directory")).toHaveValue("/backups/");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(mocks.mutations).toContainEqual({
      route: "schedules.update",
      request: {
        id: "backup",
        options: {
          ...status.definition.options,
          on_calendar: "Sat *-*-* 07:00:00",
          working_directory: "/backups/",
        },
      },
    });
  });

  it("stops the exact invocation and navigates default logs by unit", async () => {
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await waitFor(() => expect(screen.getByText(/success/)).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Stop" }));
    await user.click(screen.getByRole("button", { name: "Logs" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.stop",
      request: { id: "backup", invocation_id: status.invocation_id },
    });
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/logs",
      search: { unit: status.unit_name },
    });
  });
});
