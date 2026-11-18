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
const { registerToastLinkRouter } = await import("@/api/toast-meta");
const {
  TASK_TYPE_DOCKER_UPDATE,
  TASK_TYPE_DOCKER_COMPOSE,
  TASK_TYPE_DOCKER_STORAGE,
  TASK_TYPE_FILE_DELETE_BATCH,
  TASK_TYPE_MEMORY_DRAIN,
  TASK_TYPE_MEMORY_SWAP_MODE,
  TASK_TYPE_PACKAGE_UPDATE,
  TASK_TYPE_STORAGE_BTRFS_BALANCE,
  TASK_TYPE_STORAGE_BTRFS_SCRUB,
  TASK_TYPE_STORAGE_SMART_TEST,
  TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
} = await import("./taskTypes");
const {
  claimTerminalFeedback,
  emitTerminalTaskFeedback,
  GENERIC_TASK_FEEDBACK,
  hasTerminalFeedbackOwner,
  markTerminalFeedbackEmitted,
  resetTerminalTaskFeedback,
  terminalSnapshotOutcome,
} = await import("./terminalTaskFeedback");

const deps = {
  navigate: vi.fn(async () => undefined),
  refreshCapabilities: vi.fn(async () => undefined),
};

let releaseLinkRouter = () => {};

/** Puts the user on pathname as far as toast links can tell. */
function onPage(pathname: string) {
  const link = { navigate: vi.fn(), pathname: () => pathname };
  releaseLinkRouter = registerToastLinkRouter(link);
  return link;
}

function clickAction(opts: unknown) {
  const { action } = opts as { action: { onClick: (event: unknown) => void } };
  action.onClick({});
}

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
    releaseLinkRouter();
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

  it("records Docker update completion and failure feedback", () => {
    const link = onPage("/");
    const docker = {
      action: { label: "Open Docker", onClick: expect.any(Function) },
      duration: 10_000,
    };
    emitTerminalTaskFeedback(
      task("docker-updated", TASK_TYPE_DOCKER_UPDATE),
      {
        kind: "completed",
        result: { containerName: "homepage", updated: true },
      },
      deps,
    );
    expect(toastMocks.success).toHaveBeenCalledWith("homepage updated", docker);

    emitTerminalTaskFeedback(
      task("docker-current", TASK_TYPE_DOCKER_UPDATE),
      {
        kind: "completed",
        result: { containerName: "pihole", updated: false },
      },
      deps,
    );
    expect(toastMocks.success).toHaveBeenCalledWith(
      "pihole is already up to date",
      docker,
    );

    emitTerminalTaskFeedback(
      task("docker-failed", TASK_TYPE_DOCKER_UPDATE),
      failed("registry unavailable"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith(
      "registry unavailable",
      docker,
    );

    clickAction(toastMocks.error.mock.calls[0][1]);
    expect(link.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/docker" }),
    );
  });

  it("offers no page link to a user already on that page", () => {
    onPage("/docker/containers");

    emitTerminalTaskFeedback(
      task("docker-here", TASK_TYPE_DOCKER_UPDATE),
      {
        kind: "completed",
        result: { containerName: "homepage", updated: true },
      },
      deps,
    );

    expect(toastMocks.success).toHaveBeenCalledWith(
      "homepage updated",
      undefined,
    );
  });

  it("links a zram install to the Memory overflow dialog", () => {
    const link = onPage("/settings");

    emitTerminalTaskFeedback(
      task("cap-zram", TASK_TYPE_SYSTEM_INSTALL_CAPABILITY, {
        capability: "zram",
      }),
      { kind: "completed", result: { available: true } },
      deps,
    );
    expect(toastMocks.success).toHaveBeenCalledWith("zram installed", {
      action: { label: "Open Memory overflow", onClick: expect.any(Function) },
      duration: 10_000,
    });
    clickAction(toastMocks.success.mock.calls[0][1]);
    expect(link.navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        search: { dialog: "memory-overflow" },
        to: "/",
      }),
    );
  });

  it("reports capability installs on every terminal state, cancel included", () => {
    const link = onPage("/");
    const capability = (id: string) =>
      task(id, TASK_TYPE_SYSTEM_INSTALL_CAPABILITY, { capability: "libvirt" });
    const vms = {
      action: { label: "Open VMs", onClick: expect.any(Function) },
      duration: 10_000,
    };

    emitTerminalTaskFeedback(
      capability("cap-ok"),
      { kind: "completed", result: { available: true } },
      deps,
    );
    expect(deps.refreshCapabilities).toHaveBeenCalledOnce();
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Virtual machines installed",
      vms,
    );
    clickAction(toastMocks.success.mock.calls[0][1]);
    expect(link.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/vm" }),
    );

    emitTerminalTaskFeedback(
      capability("cap-warning"),
      {
        kind: "completed",
        result: {
          available: true,
          warning: "nss-mdns was not available; EPEL may be required",
        },
      },
      deps,
    );
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Virtual machines installed with warning",
      {
        ...vms,
        description: "nss-mdns was not available; EPEL may be required",
      },
    );

    emitTerminalTaskFeedback(
      capability("cap-unavailable"),
      { kind: "completed", result: { available: false, error: "no kvm" } },
      deps,
    );
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Virtual machines installed but is still unavailable: no kvm",
      vms,
    );

    emitTerminalTaskFeedback(capability("cap-failed"), failed(""), deps);
    expect(toastMocks.error).toHaveBeenCalledWith(
      "Failed to install Virtual machines",
      vms,
    );

    // An install canceled elsewhere still stopped midway — deliberately an
    // error toast, unlike package updates.
    emitTerminalTaskFeedback(capability("cap-canceled"), canceled, deps);
    expect(toastMocks.error).toHaveBeenCalledWith("operation aborted", vms);
  });

  it("keeps SMART tests silent — the owning page fires its own toast", () => {
    emitTerminalTaskFeedback(
      task("smart-failed", TASK_TYPE_STORAGE_SMART_TEST),
      failed("self-test failed"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("keeps balances silent — BtrfsDevicesCard toasts the outcome", () => {
    emitTerminalTaskFeedback(
      task("balance-failed", TASK_TYPE_STORAGE_BTRFS_BALANCE),
      failed("no space"),
      deps,
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("keeps scrubs silent — BtrfsHealthBanner toasts the outcome", () => {
    emitTerminalTaskFeedback(
      task("scrub-failed", TASK_TYPE_STORAGE_BTRFS_SCRUB),
      failed("device gone"),
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

  it("offers the real folder when the generic fallback reports a refused link", () => {
    const refusal =
      "forbidden: /home/a/docs is a link to /mnt/data/docs. Open that folder to make this change";
    emitTerminalTaskFeedback(
      task("delete-1", TASK_TYPE_FILE_DELETE_BATCH),
      failed(refusal),
      deps,
      GENERIC_TASK_FEEDBACK,
    );

    expect(toastMocks.error).toHaveBeenCalledWith(
      refusal,
      expect.objectContaining({
        action: expect.objectContaining({ label: "Open the real folder" }),
      }),
    );
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

    const interrupted = terminalSnapshotOutcome({
      ...base,
      state: "interrupted",
    });
    expect(interrupted?.kind).toBe("failed");
    expect(
      interrupted && "error" in interrupted && interrupted.error,
    ).toMatchObject({ message: "interrupted" });

    expect(
      terminalSnapshotOutcome({ ...base, state: "completed", result: 1 }),
    ).toEqual({ kind: "completed", result: 1 });
    expect(terminalSnapshotOutcome({ ...base, state: "running" })).toBeNull();
  });

  it("reports a memory mode change nobody follows, with what it left", () => {
    onPage("/docker");
    const status = { left: [], mode: "zram" };
    emitTerminalTaskFeedback(
      task("m1", TASK_TYPE_MEMORY_SWAP_MODE, { action: "zram" }),
      { kind: "completed", result: status },
      deps,
    );
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Memory overflow setting applied",
      expect.anything(),
    );

    emitTerminalTaskFeedback(
      task("m2", TASK_TYPE_MEMORY_SWAP_MODE, { action: "zram" }),
      {
        kind: "completed",
        result: {
          left: [{ reason: "not_enough_memory", what: "swap_file" }],
          mode: "mixed",
        },
      },
      deps,
    );
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Not enough free memory to bring back the data in the swap file, so it stays on. Apply again when more memory is free.",
      expect.anything(),
    );

    emitTerminalTaskFeedback(
      task("m3", TASK_TYPE_MEMORY_SWAP_MODE, { action: "zram" }),
      failed("another memory change is running"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith(
      "another memory change is running",
      expect.anything(),
    );
  });

  it("stays silent for a memory change a dialog follows", () => {
    markTerminalFeedbackEmitted("m4");
    emitTerminalTaskFeedback(
      task("m4", TASK_TYPE_MEMORY_SWAP_MODE, { action: "zram" }),
      { kind: "completed", result: { left: [], mode: "zram" } },
      deps,
    );
    expect(toastMocks.success).not.toHaveBeenCalled();
  });

  it("reports a drain nobody follows", () => {
    const link = onPage("/docker");
    emitTerminalTaskFeedback(
      task("d1", TASK_TYPE_MEMORY_DRAIN),
      { kind: "completed", result: {} },
      deps,
    );
    // The toast opens the Memory overflow dialog, by its URL.
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Parked data brought back to memory",
      {
        action: {
          label: "Open Memory overflow",
          onClick: expect.any(Function),
        },
        duration: 10_000,
      },
    );
    clickAction(toastMocks.success.mock.calls[0][1]);
    expect(link.navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        search: { dialog: "memory-overflow" },
        to: "/",
      }),
    );
    emitTerminalTaskFeedback(
      task("d2", TASK_TYPE_MEMORY_DRAIN),
      failed("not enough free memory"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith(
      "not enough free memory",
      expect.anything(),
    );
  });

  it("reports a Docker storage change nobody follows, and stays silent for one a section follows", () => {
    onPage("/docker");
    emitTerminalTaskFeedback(
      task("s1", TASK_TYPE_DOCKER_STORAGE),
      { kind: "completed", result: {} },
      deps,
    );
    expect(toastMocks.success).toHaveBeenCalledWith(
      "Docker restarted on the new storage. Deploy your stacks again.",
    );
    emitTerminalTaskFeedback(
      task("s2", TASK_TYPE_DOCKER_STORAGE),
      failed("docker did not start"),
      deps,
    );
    expect(toastMocks.error).toHaveBeenCalledWith("docker did not start");

    toastMocks.success.mockClear();
    markTerminalFeedbackEmitted("s3");
    emitTerminalTaskFeedback(
      task("s3", TASK_TYPE_DOCKER_STORAGE),
      { kind: "completed", result: {} },
      deps,
    );
    expect(toastMocks.success).not.toHaveBeenCalled();
  });
});
