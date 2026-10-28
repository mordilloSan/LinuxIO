import { useState } from "react";

import { linuxio } from "@/api";

import { type VMAction, VM_TOAST } from "./vmShared";

export const ACTION_PROGRESS_LABEL: Record<VMAction, string> = {
  start: "Starting",
  shutdown: "Shutting down",
  reboot: "Rebooting",
  force_off: "Powering off",
  suspend: "Suspending",
  resume: "Resuming",
};

// libvirt acknowledges a lifecycle request before the guest acts on it, so the
// polled state lags the call by seconds (or forever, if the guest ignores
// ACPI). A null target settles as soon as the call returns.
const SETTLED_STATE: Record<VMAction, string | null> = {
  start: "running",
  shutdown: "shutoff",
  reboot: null,
  force_off: "shutoff",
  suspend: "paused",
  resume: "running",
};
const SETTLE_TIMEOUT_MS = 60_000;

/**
 * Lifecycle mutations plus the per-machine pending map. A machine stays
 * pending until `vms` reports the state its action leads to, the machine
 * disappears, the call fails, or the settle timeout elapses.
 */
export function useVMLifecycle(
  vms: readonly { name: string; state: string }[],
) {
  const [pendingActions, setPendingActions] = useState<
    ReadonlyMap<string, VMAction>
  >(() => new Map());

  const actionConfig = (success: string, error: string) => ({
    success,
    error,
    toast: VM_TOAST,
  });
  const mutations: Record<
    VMAction,
    { mutateAsync: (request: { name: string }) => Promise<unknown> }
  > = {
    start: linuxio.virt.start.useMutation(
      actionConfig("VM started", "Failed to start VM"),
    ),
    shutdown: linuxio.virt.shutdown.useMutation(
      actionConfig("VM shutdown requested", "Failed to shutdown VM"),
    ),
    reboot: linuxio.virt.reboot.useMutation(
      actionConfig("VM reboot requested", "Failed to reboot VM"),
    ),
    force_off: linuxio.virt.force_off.useMutation(
      actionConfig("VM powered off", "Failed to force off VM"),
    ),
    suspend: linuxio.virt.suspend.useMutation(
      actionConfig("VM suspended", "Failed to suspend VM"),
    ),
    resume: linuxio.virt.resume.useMutation(
      actionConfig("VM resumed", "Failed to resume VM"),
    ),
  };

  const clearPending = (name: string, action: VMAction) =>
    setPendingActions((current) => {
      if (current.get(name) !== action) return current;
      const next = new Map(current);
      next.delete(name);
      return next;
    });

  // Adjust during render, so a poll reporting the target state (or dropping
  // the machine) settles the row in the same pass.
  const settled = [...pendingActions].filter(([name, action]) => {
    const vm = vms.find((candidate) => candidate.name === name);
    const target = SETTLED_STATE[action];
    return !vm || (target !== null && vm.state === target);
  });
  if (settled.length > 0) {
    const next = new Map(pendingActions);
    for (const [name] of settled) next.delete(name);
    setPendingActions(next);
  }

  const runAction = (action: VMAction, vm: { name: string }) => {
    const { name } = vm;
    if (pendingActions.has(name)) return;
    setPendingActions((current) => new Map(current).set(name, action));
    mutations[action].mutateAsync({ name }).then(
      () => {
        if (SETTLED_STATE[action] === null) {
          clearPending(name, action);
          return;
        }
        // A late clear after unmount is a no-op setState.
        setTimeout(() => clearPending(name, action), SETTLE_TIMEOUT_MS);
      },
      () => clearPending(name, action),
    );
  };

  return { pendingActions, runAction };
}
