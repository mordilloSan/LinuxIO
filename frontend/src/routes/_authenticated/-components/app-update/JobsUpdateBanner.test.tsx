import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskSnapshot } from "@/api/generated/linuxio-types";
import { taskTitle } from "@/tasks/taskTitles";
import { render, screen, within } from "@/test/render";
import { formatDate } from "@/utils/formatters";

import JobsUpdateBanner from "./JobsUpdateBanner";

const mocks = vi.hoisted(() => ({
  status: {
    updatePending: true,
    running: 0,
    jobs: [] as TaskSnapshot[],
  },
}));

vi.mock("./useJobHostStatus", () => ({
  useJobHostStatus: () => mocks.status,
}));

const job = (overrides: Partial<TaskSnapshot> = {}): TaskSnapshot => ({
  created_at: "2026-11-10T10:00:00Z",
  id: "job-1",
  owner: { username: "alice" },
  started_at: "2026-11-10T10:01:00Z",
  state: "running",
  type: "storage.btrfs_scrub_start",
  updated_at: "2026-11-10T10:01:00Z",
  ...overrides,
});

describe("JobsUpdateBanner", () => {
  beforeEach(() => {
    mocks.status = { updatePending: true, running: 0, jobs: [] };
  });

  it("renders nothing when no update waits", () => {
    mocks.status = { updatePending: false, running: 0, jobs: [] };

    const { container } = render(<JobsUpdateBanner />);

    expect(container).toBeEmptyDOMElement();
  });

  it("says how many running jobs the update waits for", () => {
    mocks.status = { updatePending: true, running: 2, jobs: [] };

    render(<JobsUpdateBanner />);

    expect(
      screen.getByText(
        "LinuxIO was updated. New jobs wait for 2 running jobs to finish.",
      ),
    ).toBeInTheDocument();
  });

  it("uses the singular for one running job", () => {
    mocks.status = { updatePending: true, running: 1, jobs: [] };

    render(<JobsUpdateBanner />);

    expect(
      screen.getByText(
        "LinuxIO was updated. New jobs wait for 1 running job to finish.",
      ),
    ).toBeInTheDocument();
  });

  it("says new jobs can start shortly when none is running, with no button", () => {
    render(<JobsUpdateBanner />);

    expect(
      screen.getByText("LinuxIO was updated. New jobs can start shortly."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows no button when jobs are running but none is listed", () => {
    mocks.status = { updatePending: true, running: 3, jobs: [] };

    render(<JobsUpdateBanner />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("unfolds the running jobs under the text and folds them again", async () => {
    const first = job();
    const second = job({
      id: "job-2",
      metadata: { label: "Rebuild the index" },
      owner: undefined,
      started_at: undefined,
    });
    mocks.status = { updatePending: true, running: 2, jobs: [first, second] };

    const { user } = render(<JobsUpdateBanner />);

    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    const show = screen.getByRole("button", { name: "Show running jobs" });
    expect(show).toHaveAttribute("aria-expanded", "false");

    await user.click(show);

    const hide = screen.getByRole("button", { name: "Hide running jobs" });
    expect(hide).toHaveAttribute("aria-expanded", "true");
    const items = within(screen.getByRole("list")).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent(taskTitle(first));
    expect(items[0]).toHaveTextContent("alice");
    expect(items[0]).toHaveTextContent(formatDate(first.started_at));
    expect(items[1]).toHaveTextContent("Rebuild the index");
    expect(items[1]).not.toHaveTextContent("Unknown");

    await user.click(hide);

    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Show running jobs" }),
    ).toBeInTheDocument();
  });

  it("is a status message, not an alert that interrupts", () => {
    mocks.status = { updatePending: true, running: 1, jobs: [] };

    render(<JobsUpdateBanner />);

    expect(screen.getByRole("status")).toHaveTextContent(
      "LinuxIO was updated. New jobs wait for 1 running job to finish.",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps a long list of jobs to a bounded height that scrolls", async () => {
    const jobs = Array.from({ length: 30 }, (_, index) =>
      job({ id: `job-${index}` }),
    );
    mocks.status = { updatePending: true, running: jobs.length, jobs };
    const { user } = render(<JobsUpdateBanner />);

    await user.click(screen.getByRole("button", { name: "Show running jobs" }));

    // Read from the inline style: the computed value resolves min().
    const list = screen.getByRole("list");
    expect(list.style.maxHeight).not.toBe("");
    expect(list.style.overflowY).toBe("auto");
  });

  it("ends a job's line with its progress when it reports a percentage", async () => {
    const jobs = [
      job({
        id: "job-1",
        progress: { percentage: 42.6, message: "Scrubbing" },
      }),
      job({ id: "job-2", progress: { message: "Starting" } }),
      job({ id: "job-3" }),
    ];
    mocks.status = { updatePending: true, running: 3, jobs };
    const { user } = render(<JobsUpdateBanner />);

    await user.click(screen.getByRole("button", { name: "Show running jobs" }));

    const items = within(screen.getByRole("list")).getAllByRole("listitem");
    expect(items[0].textContent).toBe(
      `${taskTitle(jobs[0])} · alice · started ${formatDate(jobs[0].started_at)} · 43%`,
    );
    expect(items[1]).not.toHaveTextContent("%");
    expect(items[2]).not.toHaveTextContent("%");
  });

  it("offers nothing but the list toggle", () => {
    mocks.status = { updatePending: true, running: 1, jobs: [job()] };

    render(<JobsUpdateBanner />);

    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("drops the list and the button when the jobs end while it is open", async () => {
    mocks.status = { updatePending: true, running: 1, jobs: [job()] };
    const { user, rerender } = render(<JobsUpdateBanner />);
    await user.click(screen.getByRole("button", { name: "Show running jobs" }));

    mocks.status = { updatePending: true, running: 0, jobs: [] };
    rerender(<JobsUpdateBanner />);

    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(
      screen.getByText("LinuxIO was updated. New jobs can start shortly."),
    ).toBeInTheDocument();
  });
});
