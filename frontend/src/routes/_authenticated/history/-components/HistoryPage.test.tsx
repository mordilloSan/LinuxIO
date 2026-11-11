import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskHistory, TaskHistoryEntry, TaskSnapshot } from "@/api";
import { LinuxIOError } from "@/api/errors";
import { ConfigContext } from "@/contexts/ConfigContext";
import {
  act,
  createConfigContextValue,
  renderWithTanStackRouter,
  screen,
  within,
} from "@/test/render";
import { formatDate } from "@/utils/formatters";

import HistoryPage from "./HistoryPage";

const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  updateConfig: vi.fn(),
  updatePending: false,
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

vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: mocks.call,
}));

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const entry = (
  id: string,
  task: Partial<TaskSnapshot> = {},
  rest: Partial<TaskHistoryEntry> = {},
): TaskHistoryEntry => ({
  task: {
    id,
    // A job whose label says what it did, so the label is the row's title.
    type: "storage.btrfs_scrub_start",
    state: "completed",
    created_at: "2026-11-10T09:59:00Z",
    started_at: "2026-11-10T10:00:00Z",
    finished_at: "2026-11-10T10:02:05Z",
    updated_at: "2026-11-10T10:02:05Z",
    owner: { username: "alice" },
    metadata: { label: `Job ${id}` },
    ...task,
  },
  ...rest,
});

const history = (entries: TaskHistoryEntry[], more = false): TaskHistory => ({
  entries,
  more,
});

function answer(pages: TaskHistory[], output = "saved text") {
  let page = 0;
  mocks.call.mockImplementation(async (route: string) => {
    if (route === "tasks.output") {
      return { content: output, offset: 0, size: output.length };
    }
    return pages[Math.min(page++, pages.length - 1)];
  });
}

const config = createConfigContextValue({ updateConfig: mocks.updateConfig });

const page = (
  props: Partial<React.ComponentProps<typeof HistoryPage>> = {},
) => (
  <ConfigContext.Provider value={config}>
    <HistoryPage onSelectedChange={vi.fn()} {...props} />
  </ConfigContext.Provider>
);

// Let queries settle and effects run, for tests that assert nothing happened.
const settle = () => act(() => new Promise((resolve) => setTimeout(resolve)));

const renderPage = (
  props: Partial<React.ComponentProps<typeof HistoryPage>> = {},
  privileged = false,
) => renderWithTanStackRouter(page(props), { auth: { privileged } });

describe("HistoryPage", () => {
  beforeEach(() => {
    mocks.call.mockReset();
    mocks.updateConfig.mockReset();
    mocks.updatePending = false;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("stores when the history was seen, once per visit", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-11T08:00:00.000Z"));
    answer([history([entry("a"), entry("b")])]);

    const { rerender, user } = renderPage();
    await user.click(await screen.findByText("Job a"));
    rerender(page({ selected: "a" }));
    await screen.findAllByText("Job a");

    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
    expect(mocks.updateConfig).toHaveBeenCalledWith({
      dismissals: { historySeenAt: "2026-11-11T08:00:00.000Z" },
    });
  });

  it("stores the time again on the next visit", async () => {
    answer([history([entry("a")])]);

    const first = renderPage();
    await screen.findByText("Job a");
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
    first.unmount();

    renderPage();
    await screen.findByText("Job a");
    expect(mocks.updateConfig).toHaveBeenCalledTimes(2);
  });

  it("stores no time while an update waits and the history is unavailable", async () => {
    mocks.updatePending = true;
    answer([history([])]);

    renderPage();

    await screen.findByText(
      "History is unavailable until the LinuxIO update finishes.",
    );
    await settle();
    expect(mocks.updateConfig).not.toHaveBeenCalled();
  });

  it("stores the time once the update has finished and the history is shown", async () => {
    mocks.updatePending = true;
    answer([history([entry("a")])]);

    const { rerender } = renderPage();
    await screen.findByText("Job a");
    await settle();
    expect(mocks.updateConfig).not.toHaveBeenCalled();

    mocks.updatePending = false;
    rerender(page());
    await settle();
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1);
  });

  it("stores no time when the history fails to load", async () => {
    mocks.call.mockRejectedValue(new LinuxIOError("job service down", 503));

    renderPage();

    await screen.findByText(/job service down/);
    await settle();
    expect(mocks.updateConfig).not.toHaveBeenCalled();
  });

  it("stores no time for a history with nothing in it to have seen", async () => {
    answer([history([])]);

    renderPage();

    await screen.findByText("Nothing has ended yet.");
    await settle();
    expect(mocks.updateConfig).not.toHaveBeenCalled();
  });

  it("says only that the history is unavailable for a selected job while an update waits", async () => {
    mocks.updatePending = true;
    answer([history([])]);

    renderPage({ selected: "gone" });

    expect(
      await screen.findByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeVisible();
    await settle();
    expect(screen.queryByText(/not among the loaded entries/)).toBeNull();
  });

  it("says a selected job is not among the loaded entries, and to load older", async () => {
    answer([history([entry("a")], true)]);

    renderPage({ selected: "gone" });

    expect(
      await screen.findByText(
        "This job is not among the loaded entries. Load older to look for it.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Load older" })).toBeVisible();
  });

  it("says a selected job is not among the loaded entries when none are older", async () => {
    answer([history([entry("a")])]);

    renderPage({ selected: "gone" });

    expect(
      await screen.findByText("This job is not among the loaded entries."),
    ).toBeVisible();
    expect(screen.queryByText(/Load older to look for it/)).toBeNull();
  });

  it("finds a selected job once it is loaded", async () => {
    answer([
      history([entry("a")], true),
      history([
        entry("older", { state: "failed", error: { message: "gone wrong" } }),
      ]),
    ]);

    const { user } = renderPage({ selected: "older" });
    await screen.findByText(
      "This job is not among the loaded entries. Load older to look for it.",
    );

    await user.click(screen.getByRole("button", { name: "Load older" }));

    expect(await screen.findByText("gone wrong")).toBeVisible();
    expect(screen.queryByText(/not among the loaded entries/)).toBeNull();
  });

  it("lists what ended with its outcome, owner, end time and run time", async () => {
    answer([
      history([
        entry("b"),
        entry(
          "a",
          {
            state: "interrupted",
            owner: { username: "bob" },
            finished_at: "2026-11-10T09:00:30Z",
            started_at: "2026-11-10T09:00:00Z",
          },
          { reason: "reboot" },
        ),
      ]),
    ]);

    renderPage();

    const rows = await screen.findAllByRole("row");
    const text = rows.map((row) => row.textContent ?? "");
    const first = text.findIndex((value) => value.includes("Job b"));
    const second = text.findIndex((value) => value.includes("Job a"));
    expect(first).toBeGreaterThan(-1);
    expect(second).toBeGreaterThan(first);
    const firstRow = within(rows[first]);
    expect(firstRow.getByText("Completed")).toBeInTheDocument();
    expect(firstRow.getByText("alice")).toBeInTheDocument();
    expect(
      firstRow.getByText(formatDate("2026-11-10T10:02:05Z")),
    ).toBeInTheDocument();
    expect(firstRow.getByText("2m 5s")).toBeInTheDocument();
    const secondRow = within(rows[second]);
    expect(secondRow.getByText("Interrupted (reboot)")).toBeInTheDocument();
    expect(secondRow.getByText("bob")).toBeInTheDocument();
    expect(secondRow.getByText("30s")).toBeInTheDocument();
  });

  it("shows nothing for a run time it cannot work out", async () => {
    answer([history([entry("a", { started_at: undefined })])]);

    renderPage();

    const row = (await screen.findByText("Job a")).closest('[role="row"]');
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).queryByText(/\d+s$/)).toBeNull();
  });

  it("says so when nothing has ended", async () => {
    answer([history([])]);

    renderPage();

    expect(await screen.findByText("Nothing has ended yet.")).toBeVisible();
  });

  it("says history is unavailable, not empty, while an update waits", async () => {
    mocks.updatePending = true;
    answer([history([])]);

    renderPage();

    expect(
      await screen.findByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeVisible();
    expect(screen.queryByText("Nothing has ended yet.")).toBeNull();
  });

  it("still lists the entries it has while an update waits", async () => {
    mocks.updatePending = true;
    answer([history([entry("a")])]);

    renderPage();

    expect(await screen.findByText("Job a")).toBeVisible();
    expect(
      screen.queryByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeNull();
  });

  it("shows a failed query as an error, not as an unavailable history", async () => {
    mocks.updatePending = true;
    mocks.call.mockRejectedValue(new LinuxIOError("job service down", 503));

    renderPage();

    expect(await screen.findByText(/job service down/)).toBeVisible();
    expect(
      screen.queryByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeNull();
  });

  it("shows a failed query as an error, not as an empty history", async () => {
    mocks.call.mockRejectedValue(new LinuxIOError("job service down", 503));

    renderPage();

    expect(await screen.findByText(/job service down/)).toBeVisible();
    expect(screen.queryByText("Nothing has ended yet.")).toBeNull();
  });

  it("marks each row with how its run ended", async () => {
    answer([
      history([
        entry("ok"),
        entry("bad", { state: "failed" }),
        entry("stopped", { state: "canceled" }),
      ]),
    ]);

    renderPage();

    const level = async (name: string) =>
      (await screen.findByText(name))
        .closest('[role="row"]')
        ?.querySelector(".log-level")
        ?.getAttribute("data-level");
    expect(await level("Job ok")).toBe("info");
    expect(await level("Job bad")).toBe("error");
    expect(await level("Job stopped")).toBe("warn");
  });

  it("narrows the table to one outcome and counts what is shown", async () => {
    answer([history([entry("ok"), entry("bad", { state: "failed" })])]);

    const { user } = renderPage();
    await screen.findByText("Job ok");
    expect(screen.getByText("2 shown")).toBeVisible();

    await user.click(screen.getByRole("combobox", { name: "Outcome" }));
    await user.click(await screen.findByRole("option", { name: "Failed" }));

    expect(screen.getByText("Job bad")).toBeVisible();
    expect(screen.queryByText("Job ok")).toBeNull();
    expect(screen.getByText("1 shown")).toBeVisible();

    await user.click(screen.getByRole("combobox", { name: "Outcome" }));
    await user.click(await screen.findByRole("option", { name: "Cancelled" }));

    expect(await screen.findByText("No matching jobs.")).toBeVisible();
  });

  it("loads older entries from the exact end time it was given", async () => {
    const cursor = "2026-11-10T10:02:05.123456789Z";
    answer([
      history([entry("b"), entry("a", { finished_at: cursor })], true),
      history([entry("older")]),
    ]);

    const { user } = renderPage();
    await screen.findByText("Job a");

    await user.click(screen.getByRole("button", { name: "Load older" }));

    expect(await screen.findByText("Job older")).toBeVisible();
    expect(screen.getByText("Job b")).toBeVisible();
    expect(mocks.call.mock.calls[0].slice(0, 2)).toEqual(["tasks.history", {}]);
    expect(mocks.call.mock.calls[1].slice(0, 2)).toEqual([
      "tasks.history",
      { before: cursor },
    ]);
    expect(screen.queryByRole("button", { name: "Load older" })).toBeNull();
  });

  it("offers no older entries when the server has no more", async () => {
    answer([history([entry("a")])]);

    renderPage();
    await screen.findByText("Job a");

    expect(screen.queryByRole("button", { name: "Load older" })).toBeNull();
  });

  it("selects an entry when its row is pressed", async () => {
    answer([history([entry("a")])]);
    const onSelectedChange = vi.fn();

    const { user } = renderPage({ onSelectedChange });
    await user.click(await screen.findByText("Job a"));

    expect(onSelectedChange).toHaveBeenCalledWith("a");
  });

  it("highlights the row of the selected entry", async () => {
    answer([history([entry("a"), entry("b")])]);

    renderPage({ selected: "b" });

    const row = (name: string) =>
      screen.getAllByText(name)[0].closest('[role="row"]');
    await screen.findAllByText("Job b");
    expect(row("Job b")).toHaveClass("app-dt__row--selected");
    expect(row("Job a")).not.toHaveClass("app-dt__row--selected");
  });

  it("shows the error of a selected failed entry", async () => {
    answer([
      history([
        entry("a", {
          state: "failed",
          error: { message: "disk is full" },
        }),
      ]),
    ]);

    renderPage({ selected: "a" });

    expect(await screen.findByText("disk is full")).toBeVisible();
  });

  it("shows the saved output of a scheduled run to a privileged session", async () => {
    answer([
      history(
        [entry("a", { type: "scheduled.script" }, { output: true })],
        false,
      ),
    ]);

    renderPage({ selected: "a" }, true);

    expect(await screen.findByText("saved text")).toBeVisible();
    expect(
      mocks.call.mock.calls.filter(([route]) => route === "tasks.output"),
    ).toHaveLength(1);
  });

  it("shows no output section to an unprivileged session", async () => {
    answer([
      history([entry("a", { type: "scheduled.script" }, { output: true })]),
    ]);

    renderPage({ selected: "a" }, false);

    await screen.findByRole("link", { name: "Open" });
    expect(screen.queryByText("saved text")).toBeNull();
    expect(mocks.call.mock.calls.map(([route]) => route)).not.toContain(
      "tasks.output",
    );
  });

  it("links a scheduled run to the page that owns it", async () => {
    answer([
      history([
        entry("a", {
          type: "scheduled.script",
          metadata: { label: "Nightly", identity: ["backup"] },
        }),
      ]),
    ]);

    renderPage({ selected: "a" });

    const link = await screen.findByRole("link", { name: "Open" });
    expect(link).toHaveAttribute(
      "href",
      expect.stringContaining("/services/schedules"),
    );
  });
});
