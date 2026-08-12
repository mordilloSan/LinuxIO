import { afterEach, describe, expect, it, vi } from "vitest";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

const { LinuxIOError } = await import("@/api");
const {
  TASK_TYPE_DOCKER_COMPOSE,
  TASK_TYPE_PACKAGE_UPDATE,
  TASK_TYPE_STORAGE_SMART_TEST,
  TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
} = await import("@/constants/backgroundTaskTypes");
const {
  claimTerminalFeedback,
  emitTerminalTaskFeedback,
  GENERIC_TASK_FEEDBACK,
  hasTerminalFeedbackOwner,
  markTerminalFeedbackEmitted,
  resetTerminalTaskFeedback,
  terminalSnapshotOutcome,
} = await import("./terminalTaskFeedback");

const deps = { refreshCapabilities: vi.fn(async () => undefined) };

function task(
  id: string,
  type: string,
  metadata: Record<string, unknown> = {},
) {
  return { id, type, metadata };
}

function failed(message: string, code?: number) {
  return { kind: "failed" as const, error: new LinuxIOError(message, code) };
}

const canceled = {
  kind: "canceled" as const,
  error: new LinuxIOError("operation aborted", 499),
};

describe("terminalTaskFeedback", () => {
  afterEach(() => {
    resetTerminalTaskFeedback();
    vi.clearAllMocks();
  });

  it("reports each task once, whichever delivery path lands first", () => {
    const failure = task("task-1", TASK_TYPE_PACKAGE_UPDATE);
    emitTerminalTaskFeedback(failure, failed("dpkg failed"), deps);
    emitTerminalTaskFeedback(failure, failed("dpkg failed"), deps);

    expect(toastMocks.error).toHaveBeenCalledOnce();
    expect(toastMocks.error).toHaveBeenCalledWith("dpkg failed");
  });

  it("bounds the dedupe set instead of growing one id per task forever", () => {
    for (let i = 0; i <= 200; i++) {
      markTerminalFeedbackEmitted(`task-${i}`);
    }

    // The oldest id was evicted, the newest retained.
    expect(markTerminalFeedbackEmitted("task-0")).toBe(true);
    expect(markTerminalFeedbackEmitted("task-200")).toBe(false);
  });

  it("suppresses feedback while a page claims the type and resumes on release", () => {
    const release = claimTerminalFeedback(TASK_TYPE_PACKAGE_UPDATE);
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);

    emitTerminalTaskFeedback(
      task("task-owned", TASK_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    release();
    release(); // releases are idempotent
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);

    emitTerminalTaskFeedback(
      task("task-after-release", TASK_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledOnce();
  });

  it("keeps the type claimed until every claim is released", () => {
    const first = claimTerminalFeedback(TASK_TYPE_PACKAGE_UPDATE);
    const second = claimTerminalFeedback(TASK_TYPE_PACKAGE_UPDATE);

    first();
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);
    second();
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);
  });

  it("stays silent for a task whose outcome a page already painted", () => {
    markTerminalFeedbackEmitted("task-painted");

    emitTerminalTaskFeedback(
      task("task-painted", TASK_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("toasts package-update failures but never completions or cancels", () => {
    emitTerminalTaskFeedback(
      task("task-done", TASK_TYPE_PACKAGE_UPDATE),
      { kind: "completed", result: undefined },
      deps,
    );
    emitTerminalTaskFeedback(
      task("task-canceled", TASK_TYPE_PACKAGE_UPDATE),
      canceled,
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    emitTerminalTaskFeedback(
      task("task-empty-error", TASK_TYPE_PACKAGE_UPDATE),
      failed(""),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("Package update failed");
  });

  it("reports capability installs on every terminal state, cancel included", () => {
    const capability = (id: string) =>
      task(id, TASK_TYPE_SYSTEM_INSTALL_CAPABILITY, { capability: "libvirt" });
    const route = { label: "Open VMs", to: "/vm" };

    emitTerminalTaskFeedback(
      capability("cap-ok"),
      { kind: "completed", result: { available: true } },
      deps,
    );
    expect(deps.refreshCapabilities).toHaveBeenCalledOnce();
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Virtual machines installed",
      { meta: route },
    );

    emitTerminalTaskFeedback(
      capability("cap-unavailable"),
      { kind: "completed", result: { available: false, error: "no kvm" } },
      deps,
    );
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Virtual machines installed but is still unavailable: no kvm",
      { meta: route },
    );

    emitTerminalTaskFeedback(capability("cap-failed"), failed(""), deps);
    expect(toastMocks.error).toHaveBeenCalledWith(
      "Failed to install Virtual machines",
      { meta: route },
    );

    // An install canceled elsewhere still stopped midway — deliberately an
    // error toast, unlike package updates.
    emitTerminalTaskFeedback(capability("cap-canceled"), canceled, deps);
    expect(toastMocks.error).toHaveBeenCalledWith("operation aborted", {
      meta: route,
    });
  });

  it("keeps SMART tests silent — the owning page fires its own toast", () => {
    emitTerminalTaskFeedback(
      task("smart-failed", TASK_TYPE_STORAGE_SMART_TEST),
      failed("self-test failed"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("only toasts genuine failures through the generic fallback", () => {
    const compose = task("compose-1", TASK_TYPE_DOCKER_COMPOSE);
    emitTerminalTaskFeedback(compose, canceled, deps, GENERIC_TASK_FEEDBACK);
    expect(toastMocks.error).not.toHaveBeenCalled();

    emitTerminalTaskFeedback(
      task("compose-2", TASK_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
      GENERIC_TASK_FEEDBACK,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("disk full");
  });

  it("emits nothing for unregistered types without an explicit entry", () => {
    // The events fallback path passes no entry: types a page handles locally
    // must not be double-reported there.
    emitTerminalTaskFeedback(
      task("compose-3", TASK_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    // ...and the skipped emit must not consume the task's dedupe slot, or the
    // attach path arriving second would drop the only report.
    emitTerminalTaskFeedback(
      task("compose-3", TASK_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
      GENERIC_TASK_FEEDBACK,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("disk full");
  });

  it("normalizes terminal snapshots to the attach path's error shape", () => {
    const base = {
      created_at: "2026-01-01T00:00:00Z",
      id: "snap-1",
      type: TASK_TYPE_PACKAGE_UPDATE,
      updated_at: "2026-01-01T00:01:00Z",
    };

    const failure = terminalSnapshotOutcome({
      ...base,
      state: "failed",
      error: { message: "dpkg failed", code: 500 },
    });
    expect(failure?.kind).toBe("failed");
    expect(failure && "error" in failure && failure.error).toBeInstanceOf(
      LinuxIOError,
    );

    const cancel = terminalSnapshotOutcome({
      ...base,
      state: "canceled",
      error: { message: "operation aborted", code: 499 },
    });
    expect(cancel?.kind).toBe("canceled");

    expect(
      terminalSnapshotOutcome({ ...base, state: "completed", result: 1 }),
    ).toEqual({ kind: "completed", result: 1 });
    expect(terminalSnapshotOutcome({ ...base, state: "running" })).toBeNull();
  });
});
