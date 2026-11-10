import { useSyncExternalStore } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskHistoryEntry } from "@/api";
import { act, fireEvent, render, screen, waitFor } from "@/test/render";
import { formatDate } from "@/utils/formatters";

import SchedulesTab from "./SchedulesTab";

const mocks = vi.hoisted(() => ({
  mutations: [] as Array<{ route: string; request: unknown }>,
  navigate: vi.fn(),
  openChannel: vi.fn(() => null),
  history: {} as { data?: unknown; error?: Error },
  historyListeners: new Set<() => void>(),
  historyOptions: [] as Array<{
    queryKey: unknown[];
    refetchInterval?: number;
  }>,
}));

// Like a query result that changes: a rerender alone would not reach a child
// the React compiler has memoised.
const setHistory = (history: typeof mocks.history) =>
  act(() => {
    mocks.history = history;
    mocks.historyListeners.forEach((listener) => listener());
  });

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

let selectedStatus: typeof status = status;

const entry = (
  id: string,
  state: string,
  extra: Partial<TaskHistoryEntry> & { error?: string } = {},
) => ({
  task: {
    id,
    type: "schedule.run",
    state,
    created_at: "2026-11-09T10:00:00Z",
    started_at: "2026-11-09T10:00:00Z",
    finished_at: "2026-11-09T10:00:42Z",
    ...(extra.error ? { error: { message: extra.error } } : {}),
  },
  ...(extra.output === undefined ? {} : { output: extra.output }),
});
const failedRun = entry("run-failed", "failed", {
  error: "exit status 2",
  output: true,
});
const completedRun = entry("run-ok", "completed");
const runLabel = (outcome: string) =>
  `${formatDate("2026-11-09T10:00:42Z")} · ${outcome} · 42s`;

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/components/tasks/SavedOutput", () => ({
  default: ({ taskId }: { taskId: string }) => <div>Saved output {taskId}</div>,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: { available: true, error: null, schedules: [] },
    }),
    useQuery: (options: { queryKey: unknown[]; refetchInterval?: number }) => {
      const history = useSyncExternalStore(
        (listener) => {
          mocks.historyListeners.add(listener);
          return () => mocks.historyListeners.delete(listener);
        },
        () => mocks.history,
      );
      if (options.queryKey.includes("history")) {
        mocks.historyOptions.push(options);
        return history;
      }
      return {
        data: options.queryKey.some((part) => String(part).includes("get"))
          ? selectedStatus
          : [{ username: "root" }, { username: "alice" }],
      };
    },
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (
    endpoint: { route: string },
    config?: { success?: () => void },
  ) => ({
    isPending: false,
    mutate: (request: unknown) => {
      mocks.mutations.push({ route: endpoint.route, request });
      config?.success?.();
    },
  });
  return {
    ...actual,
    openChannel: mocks.openChannel,
    useStreamMux: () => ({ isOpen: true }),
    linuxio: mockMutationHooks(
      {
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
      useCallMutation,
    ),
    useCallMutation,
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
    mocks.openChannel.mockClear();
    selectedStatus = status;
    mocks.history = { data: { entries: [], more: false } };
    mocks.historyOptions = [];
    mocks.historyListeners.clear();
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

  it("follows the selected run's output live, scoped to its invocation", async () => {
    selectedStatus = {
      ...status,
      unit_name: "linuxio-schedule-x.service",
      invocation_id: "1234567890abcdef1234567890abcdef",
    };
    render(<SchedulesTab {...props} selected="backup" />);

    expect(
      await screen.findByText("Output of the latest run"),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(mocks.openChannel).toHaveBeenCalledWith(
        "logs.service.follow",
        expect.objectContaining({
          serviceName: "linuxio-schedule-x.service",
          invocationId: "1234567890abcdef1234567890abcdef",
        }),
      ),
    );
  });

  it("shows no output card while the schedule has never run", async () => {
    selectedStatus = { ...status, invocation_id: "" };
    render(<SchedulesTab {...props} selected="backup" />);

    await waitFor(() => expect(screen.getByText(/success/)).toBeVisible());
    expect(
      screen.queryByText("Output of the latest run"),
    ).not.toBeInTheDocument();
    expect(mocks.openChannel).not.toHaveBeenCalled();
  });

  it("lists a schedule's past runs with their outcome and how long they ran", async () => {
    mocks.history = {
      data: { entries: [failedRun, completedRun], more: false },
    };
    render(<SchedulesTab {...props} selected="backup" />);

    expect(
      await screen.findByRole("button", { name: runLabel("Failed") }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: runLabel("Completed") }),
    ).toBeInTheDocument();
    expect(screen.getByText("Past runs")).toBeInTheDocument();
    expect(mocks.historyOptions.at(-1)).toMatchObject({
      queryKey: ["linuxio", "tasks", "history", { unit: status.unit_name }],
      refetchInterval: 5000,
    });
  });

  it("does not ask for past runs while no schedule is selected", () => {
    render(<SchedulesTab {...props} />);

    expect(mocks.historyOptions).toEqual([]);
  });

  it("says so when the schedule has not run yet", async () => {
    render(<SchedulesTab {...props} selected="backup" />);

    expect(
      await screen.findByText("This task has not run yet."),
    ).toBeInTheDocument();
  });

  it("shows an error, not the empty text, when the history cannot be loaded", async () => {
    mocks.history = { error: new Error("bridge unavailable") };
    render(<SchedulesTab {...props} selected="backup" />);

    expect(await screen.findByText(/bridge unavailable/)).toBeInTheDocument();
    expect(
      screen.queryByText("This task has not run yet."),
    ).not.toBeInTheDocument();
    // The card shows the error itself; a poll every 5 s must not also toast it.
    expect(mocks.historyOptions.at(-1)).toMatchObject({
      meta: { silent: true },
    });
  });

  it("shows a selected failed run's error and saved output beside the live output", async () => {
    mocks.history = {
      data: { entries: [failedRun, completedRun], more: false },
    };
    const { user } = render(<SchedulesTab {...props} selected="backup" />);

    await user.click(
      await screen.findByRole("button", { name: runLabel("Failed") }),
    );

    expect(screen.getByText("exit status 2")).toBeInTheDocument();
    expect(screen.getByText("Saved output run-failed")).toBeInTheDocument();
    expect(screen.getByText("Output of the latest run")).toBeInTheDocument();
  });

  it("shows no output for a selected run that saved none", async () => {
    mocks.history = {
      data: { entries: [failedRun, completedRun], more: false },
    };
    const { user } = render(<SchedulesTab {...props} selected="backup" />);

    await user.click(
      await screen.findByRole("button", { name: runLabel("Completed") }),
    );

    expect(
      screen.getByRole("button", {
        name: runLabel("Completed"),
        pressed: true,
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Saved output/)).not.toBeInTheDocument();
  });

  it("selects a past run from the keyboard", async () => {
    mocks.history = { data: { entries: [failedRun], more: false } };
    const { user } = render(<SchedulesTab {...props} selected="backup" />);

    (await screen.findByRole("button", { name: runLabel("Failed") })).focus();
    await user.keyboard("{Enter}");

    expect(screen.getByText("Saved output run-failed")).toBeInTheDocument();
  });

  it("selects nothing once the chosen run leaves the list", async () => {
    mocks.history = { data: { entries: [failedRun], more: false } };
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await user.click(
      await screen.findByRole("button", { name: runLabel("Failed") }),
    );
    expect(screen.getByText("Saved output run-failed")).toBeInTheDocument();

    setHistory({ data: { entries: [completedRun], more: false } });

    expect(screen.queryByText(/Saved output/)).not.toBeInTheDocument();
    expect(screen.queryByText("exit status 2")).not.toBeInTheDocument();
  });

  it("clears the chosen run when another schedule is selected", async () => {
    mocks.history = { data: { entries: [failedRun], more: false } };
    const { user, rerender } = render(
      <SchedulesTab {...props} selected="backup" />,
    );
    await user.click(
      await screen.findByRole("button", { name: runLabel("Failed") }),
    );
    expect(screen.getByText("Saved output run-failed")).toBeInTheDocument();

    selectedStatus = {
      ...status,
      definition: { ...status.definition, id: "other" },
      unit_name: "linuxio-schedule-other.service",
    };
    rerender(<SchedulesTab {...props} selected="other" />);

    expect(screen.queryByText(/Saved output/)).not.toBeInTheDocument();
  });
});
