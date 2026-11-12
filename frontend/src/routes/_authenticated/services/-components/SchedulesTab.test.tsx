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
  updatePending: false,
  notifications: {} as {
    data?: unknown;
    error?: Error;
    isPending?: boolean;
  },
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
      arguments: [] as string[],
      on_calendar: "daily",
      timezone: "UTC",
      persistent: false,
      timeout_seconds: 60,
      working_directory: "",
    },
  },
  notify: "",
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

// The notification settings as the admin saved them: what each level sends
// through, and what scripts report when a task follows the default.
const channels = (on: boolean) => ({ browser: on, email: on, webhook: on });
const notificationView = ({
  info = channels(false),
  scripts = "failures",
  settingsUnreadable,
}: {
  info?: ReturnType<typeof channels>;
  scripts?: string;
  settingsUnreadable?: boolean;
} = {}) => ({
  levels: { info, warning: channels(true), error: channels(true) },
  report: { scheduled_scripts: scripts },
  ...(settingsUnreadable ? { settingsUnreadable } : {}),
});

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

vi.mock(
  "@/routes/_authenticated/-components/app-update/useJobHostStatus",
  () => ({
    useJobHostStatus: () => ({
      updatePending: mocks.updatePending,
      running: 0,
      jobs: [],
    }),
  }),
);

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
      if (options.queryKey.includes("get_notifications")) {
        return mocks.notifications;
      }
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
    mocks.updatePending = false;
    mocks.notifications = { data: notificationView({ info: channels(true) }) };
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

  it("offers what a new task reports and sends the choice with it", async () => {
    const { user } = render(<SchedulesTab {...props} />);
    await user.click(screen.getByRole("button", { name: "Create task" }));
    await user.type(screen.getByLabelText("Name"), "nightly");
    await user.click(screen.getByLabelText("Run as"));
    await user.click(screen.getByRole("option", { name: "root" }));
    expect(screen.getByLabelText("Notify me")).toHaveTextContent(
      "Use the default",
    );
    await user.click(screen.getByLabelText("Notify me"));
    expect(
      screen.getAllByRole("option").map((option) => option.textContent),
    ).toEqual(["Use the default", "Failures only", "After every run"]);
    await user.click(screen.getByRole("option", { name: "After every run" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.create",
      request: {
        options: expect.objectContaining({ name: "nightly", user: "root" }),
        notify: "every_run",
      },
    });
  });

  describe("notice hint under Notify me", () => {
    const hint =
      "Finished runs are notices, and notices are switched off for the bell, email and webhook. Tick Notices in the notification settings to hear about them.";
    const openDialog = async (choice?: string) => {
      const { user } = render(<SchedulesTab {...props} />);
      await user.click(screen.getByRole("button", { name: "Create task" }));
      if (choice) {
        await user.click(screen.getByLabelText("Notify me"));
        await user.click(screen.getByRole("option", { name: choice }));
      }
    };

    it("shows for every run while notices are off everywhere", async () => {
      mocks.notifications = { data: notificationView() };
      await openDialog("After every run");
      expect(screen.getByText(hint)).toBeInTheDocument();
    });

    it("shows for the default when the default is every run and notices are off", async () => {
      mocks.notifications = {
        data: notificationView({ scripts: "every_run" }),
      };
      await openDialog();
      expect(screen.getByText(hint)).toBeInTheDocument();
    });

    it.each(["browser", "email", "webhook"])(
      "does not show when notices go to %s",
      async (channel) => {
        mocks.notifications = {
          data: notificationView({
            info: { ...channels(false), [channel]: true },
          }),
        };
        await openDialog("After every run");
        expect(screen.queryByText(hint)).not.toBeInTheDocument();
      },
    );

    it("does not show for failures only", async () => {
      mocks.notifications = {
        data: notificationView({ scripts: "every_run" }),
      };
      await openDialog("Failures only");
      expect(screen.queryByText(hint)).not.toBeInTheDocument();
    });

    it("does not show for the default when the default is failures", async () => {
      mocks.notifications = { data: notificationView() };
      await openDialog();
      expect(screen.queryByText(hint)).not.toBeInTheDocument();
    });

    it("does not show while the settings are loading", async () => {
      mocks.notifications = { isPending: true };
      await openDialog("After every run");
      expect(screen.queryByText(hint)).not.toBeInTheDocument();
    });

    it("does not show when the settings cannot be read", async () => {
      mocks.notifications = { error: new Error("denied") };
      await openDialog("After every run");
      expect(screen.queryByText(hint)).not.toBeInTheDocument();
    });

    it("does not show when the settings file is unreadable", async () => {
      mocks.notifications = {
        data: notificationView({ settingsUnreadable: true }),
      };
      await openDialog("After every run");
      expect(screen.queryByText(hint)).not.toBeInTheDocument();
    });
  });

  // The timing controls render "daily" as "*-*-* 00:00:00" and the argument
  // text drops empty lines: an untouched form still sends the stored options.
  it("shows a task's stored choice when editing and sends it beside the stored options", async () => {
    const options = {
      ...status.definition.options,
      arguments: ["first", "", "last"],
    };
    selectedStatus = {
      ...status,
      definition: { ...status.definition, options },
      notify: "every_run",
    };
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText("Notify me")).toHaveTextContent(
      "After every run",
    );
    await user.click(screen.getByLabelText("Notify me"));
    await user.click(screen.getByRole("option", { name: "Failures only" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.update",
      request: { id: "backup", options, notify: "failures" },
    });
  });

  it("sends the calendar the timing controls render once the time changes", async () => {
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await user.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Time"), {
      target: { value: "07:00" },
    });
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.update",
      request: {
        id: "backup",
        options: {
          ...status.definition.options,
          on_calendar: "*-*-* 07:00:00",
        },
      },
    });
  });

  it("sends the stored calendar and the new arguments when only the arguments change", async () => {
    const { user } = render(<SchedulesTab {...props} selected="backup" />);
    await user.click(screen.getByRole("button", { name: "Edit" }));
    await user.type(
      screen.getByLabelText("Arguments (one per line)"),
      "--verbose",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations).toContainEqual({
      route: "schedules.update",
      request: {
        id: "backup",
        options: { ...status.definition.options, arguments: ["--verbose"] },
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

  it("says history is unavailable, not that the schedule has not run, while an update waits", async () => {
    mocks.updatePending = true;
    render(<SchedulesTab {...props} selected="backup" />);

    expect(
      await screen.findByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This task has not run yet."),
    ).not.toBeInTheDocument();
  });

  it("still lists past runs it has while an update waits", async () => {
    mocks.updatePending = true;
    mocks.history = { data: { entries: [completedRun], more: false } };
    render(<SchedulesTab {...props} selected="backup" />);

    expect(
      await screen.findByRole("button", { name: runLabel("Completed") }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).not.toBeInTheDocument();
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
