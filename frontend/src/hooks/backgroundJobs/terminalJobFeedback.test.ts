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
  JOB_TYPE_DOCKER_COMPOSE,
  JOB_TYPE_PACKAGE_UPDATE,
  JOB_TYPE_STORAGE_SMART_TEST,
  JOB_TYPE_SYSTEM_INSTALL_CAPABILITY,
} = await import("@/constants/backgroundJobTypes");
const {
  claimTerminalFeedback,
  emitTerminalJobFeedback,
  GENERIC_JOB_FEEDBACK,
  hasTerminalFeedbackOwner,
  markTerminalFeedbackEmitted,
  resetTerminalJobFeedback,
  terminalSnapshotOutcome,
} = await import("./terminalJobFeedback");

const deps = { refreshCapabilities: vi.fn(async () => undefined) };

function job(id: string, type: string, request: Record<string, unknown> = {}) {
  return { id, type, request };
}

function failed(message: string, code?: number) {
  return { kind: "failed" as const, error: new LinuxIOError(message, code) };
}

const canceled = {
  kind: "canceled" as const,
  error: new LinuxIOError("operation aborted", 499),
};

describe("terminalJobFeedback", () => {
  afterEach(() => {
    resetTerminalJobFeedback();
    vi.clearAllMocks();
  });

  it("reports each job once, whichever delivery path lands first", () => {
    const failure = job("job-1", JOB_TYPE_PACKAGE_UPDATE);
    emitTerminalJobFeedback(failure, failed("dpkg failed"), deps);
    emitTerminalJobFeedback(failure, failed("dpkg failed"), deps);

    expect(toastMocks.error).toHaveBeenCalledOnce();
    expect(toastMocks.error).toHaveBeenCalledWith("dpkg failed");
  });

  it("bounds the dedupe set instead of growing one id per job forever", () => {
    for (let i = 0; i <= 200; i++) {
      markTerminalFeedbackEmitted(`job-${i}`);
    }

    // The oldest id was evicted, the newest retained.
    expect(markTerminalFeedbackEmitted("job-0")).toBe(true);
    expect(markTerminalFeedbackEmitted("job-200")).toBe(false);
  });

  it("suppresses feedback while a page claims the type and resumes on release", () => {
    const release = claimTerminalFeedback(JOB_TYPE_PACKAGE_UPDATE);
    expect(hasTerminalFeedbackOwner(JOB_TYPE_PACKAGE_UPDATE)).toBe(true);

    emitTerminalJobFeedback(
      job("job-owned", JOB_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    release();
    release(); // releases are idempotent
    expect(hasTerminalFeedbackOwner(JOB_TYPE_PACKAGE_UPDATE)).toBe(false);

    emitTerminalJobFeedback(
      job("job-after-release", JOB_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledOnce();
  });

  it("keeps the type claimed until every claim is released", () => {
    const first = claimTerminalFeedback(JOB_TYPE_PACKAGE_UPDATE);
    const second = claimTerminalFeedback(JOB_TYPE_PACKAGE_UPDATE);

    first();
    expect(hasTerminalFeedbackOwner(JOB_TYPE_PACKAGE_UPDATE)).toBe(true);
    second();
    expect(hasTerminalFeedbackOwner(JOB_TYPE_PACKAGE_UPDATE)).toBe(false);
  });

  it("stays silent for a job whose outcome a page already painted", () => {
    markTerminalFeedbackEmitted("job-painted");

    emitTerminalJobFeedback(
      job("job-painted", JOB_TYPE_PACKAGE_UPDATE),
      failed("dpkg failed"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("toasts package-update failures but never completions or cancels", () => {
    emitTerminalJobFeedback(
      job("job-done", JOB_TYPE_PACKAGE_UPDATE),
      { kind: "completed", result: undefined },
      deps,
    );
    emitTerminalJobFeedback(
      job("job-canceled", JOB_TYPE_PACKAGE_UPDATE),
      canceled,
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    emitTerminalJobFeedback(
      job("job-empty-error", JOB_TYPE_PACKAGE_UPDATE),
      failed(""),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("Package update failed");
  });

  it("reports capability installs on every terminal state, cancel included", () => {
    const capability = (id: string) =>
      job(id, JOB_TYPE_SYSTEM_INSTALL_CAPABILITY, { capability: "libvirt" });
    const route = { label: "Open VMs", to: "/vm" };

    emitTerminalJobFeedback(
      capability("cap-ok"),
      { kind: "completed", result: { available: true } },
      deps,
    );
    expect(deps.refreshCapabilities).toHaveBeenCalledOnce();
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Virtual machines installed",
      { meta: route },
    );

    emitTerminalJobFeedback(
      capability("cap-unavailable"),
      { kind: "completed", result: { available: false, error: "no kvm" } },
      deps,
    );
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Virtual machines installed but is still unavailable: no kvm",
      { meta: route },
    );

    emitTerminalJobFeedback(capability("cap-failed"), failed(""), deps);
    expect(toastMocks.error).toHaveBeenCalledWith(
      "Failed to install Virtual machines",
      { meta: route },
    );

    // An install canceled elsewhere still stopped midway — deliberately an
    // error toast, unlike package updates.
    emitTerminalJobFeedback(capability("cap-canceled"), canceled, deps);
    expect(toastMocks.error).toHaveBeenCalledWith("operation aborted", {
      meta: route,
    });
  });

  it("keeps SMART tests silent — the owning page fires its own toast", () => {
    emitTerminalJobFeedback(
      job("smart-failed", JOB_TYPE_STORAGE_SMART_TEST),
      failed("self-test failed"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("only toasts genuine failures through the generic fallback", () => {
    const compose = job("compose-1", JOB_TYPE_DOCKER_COMPOSE);
    emitTerminalJobFeedback(compose, canceled, deps, GENERIC_JOB_FEEDBACK);
    expect(toastMocks.error).not.toHaveBeenCalled();

    emitTerminalJobFeedback(
      job("compose-2", JOB_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
      GENERIC_JOB_FEEDBACK,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("disk full");
  });

  it("emits nothing for unregistered types without an explicit entry", () => {
    // The events fallback path passes no entry: types a page handles locally
    // must not be double-reported there.
    emitTerminalJobFeedback(
      job("compose-3", JOB_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    // ...and the skipped emit must not consume the job's dedupe slot, or the
    // attach path arriving second would drop the only report.
    emitTerminalJobFeedback(
      job("compose-3", JOB_TYPE_DOCKER_COMPOSE),
      failed("disk full"),
      deps,
      GENERIC_JOB_FEEDBACK,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("disk full");
  });

  it("normalizes terminal snapshots to the attach path's error shape", () => {
    const base = {
      created_at: "2026-01-01T00:00:00Z",
      id: "snap-1",
      type: JOB_TYPE_PACKAGE_UPDATE,
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
