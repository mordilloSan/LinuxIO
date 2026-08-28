import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi, Outlet, useParams } from "@tanstack/react-router";
import { lazy, Suspense, useCallback, useState } from "react";

import {
  linuxio,
  openVMConsoleStream,
  type VirtualMachine,
  useCallMutation,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";
import { getMutationErrorMessage } from "@/utils/mutations";

import DeleteVMDialog from "./DeleteVMDialog";
import VMListTable from "./VMListTable";
import {
  type ConsoleSession,
  type VMAction,
  VM_TOAST,
  normalizeVMDeleteResult,
} from "./vmShared";
import { VMPreflightCard } from "./VMTabs";

// noVNC only loads once a console is opened, like the docker TerminalDialog.
const ConsoleDialog = lazy(() => import("./ConsoleDialog"));

const vmMachinesRouteApi = getRouteApi("/_authenticated/vm/machines");

/**
 * Persistent shell for the Virtual machines tab.
 *
 * The list and every lifecycle mutation live here so they survive detail
 * navigation; the selected machine is a real child route rendered in the
 * outlet. Selection therefore lives in the path, not in search state.
 */
const VMMachinesLayout = () => {
  const queryClient = useQueryClient();
  const isCompactLayout = useAppMediaQuery(down("md"));
  const toast = useScopedToast(VM_TOAST);
  const navigate = vmMachinesRouteApi.useNavigate();
  // Both entries were already warmed by the /vm route loader; these observers
  // reuse them rather than issuing new requests.
  const [{ data: vms }, { data: preflight }] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchOnMount: false },
      { ...linuxio.virt.preflight({}), refetchOnMount: false },
    ],
  });
  // Undefined whenever the index child is active, i.e. no machine selected.
  const detailParams = useParams({
    from: "/_authenticated/vm/machines/$name",
    shouldThrow: false,
  });
  const selectedName = detailParams?.name ?? null;
  // Keep the delete subject separate from the URL-driven detail selection.
  // Selecting a row navigates asynchronously, so deriving this from
  // selectedName could delete the previously selected machine.
  const [deleteTargetName, setDeleteTargetName] = useState<string | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  // Deleting undefines the domain before disk cleanup finishes, so the polled
  // list can drop the row mid-action; this snapshot keeps the dialog content
  // stable until its exit animation completes.
  const [pendingDeleteVM, setPendingDeleteVM] = useState<VirtualMachine | null>(
    null,
  );
  const [consoleSession, setConsoleSession] = useState<ConsoleSession | null>(
    null,
  );
  const [consoleDialogOpen, setConsoleDialogOpen] = useState(false);
  const [pendingActions, setPendingActions] = useState<
    ReadonlyMap<string, VMAction>
  >(() => new Map());

  const setSelectedName = useCallback(
    (name: string | null) =>
      navigate(
        name
          ? { params: { name }, replace: true, to: "/vm/machines/$name" }
          : { replace: true, to: "/vm/machines" },
      ),
    [navigate],
  );
  const liveDeleteTarget =
    vms.find((vm) => vm.name === deleteTargetName) ?? null;

  const actionConfig = (successText: string, fallback: string) => ({
    success: successText,
    error: fallback,
    toast: VM_TOAST,
  });
  const deleteMutation = useCallMutation(linuxio.virt.delete, {
    invalidates: [linuxio.virt.list.queryKey],
    success: (result, request) => {
      const deleteResult = normalizeVMDeleteResult(result);
      const diskText =
        deleteResult.removed.length > 0
          ? ` Removed ${deleteResult.removed.length} disk(s).`
          : "";
      toast.success(`Deleted ${request.name}.${diskText}`);
      setDeleteDialogOpen(false);
      queryClient.removeQueries({
        queryKey: linuxio.virt.get({ name: request.name }).queryKey,
      });
      queryClient.setQueryData<VirtualMachine[]>(
        linuxio.virt.list.queryKey,
        (current) => current?.filter((vm) => vm.name !== request.name),
      );
      if (request.name === selectedName) void setSelectedName(null);
    },
    error: (error) => {
      toast.error(getMutationErrorMessage(error, "Failed to delete VM"));
    },
  });
  const deleteTarget = liveDeleteTarget ?? pendingDeleteVM;
  const startMutation = useCallMutation(
    linuxio.virt.start,
    actionConfig("VM started", "Failed to start VM"),
  );
  const shutdownMutation = useCallMutation(
    linuxio.virt.shutdown,
    actionConfig("VM shutdown requested", "Failed to shutdown VM"),
  );
  const rebootMutation = useCallMutation(
    linuxio.virt.reboot,
    actionConfig("VM reboot requested", "Failed to reboot VM"),
  );
  const forceOffMutation = useCallMutation(
    linuxio.virt.force_off,
    actionConfig("VM powered off", "Failed to force off VM"),
  );
  const suspendMutation = useCallMutation(
    linuxio.virt.suspend,
    actionConfig("VM suspended", "Failed to suspend VM"),
  );
  const resumeMutation = useCallMutation(
    linuxio.virt.resume,
    actionConfig("VM resumed", "Failed to resume VM"),
  );
  const runAction = useCallback(
    (action: VMAction, vm: VirtualMachine) => {
      if (pendingActions.has(vm.name)) return;

      const request = { name: vm.name };
      let promise: Promise<unknown>;

      setPendingActions((current) => new Map(current).set(vm.name, action));

      switch (action) {
        case "start":
          promise = startMutation.mutateAsync(request);
          break;
        case "shutdown":
          promise = shutdownMutation.mutateAsync(request);
          break;
        case "reboot":
          promise = rebootMutation.mutateAsync(request);
          break;
        case "force_off":
          promise = forceOffMutation.mutateAsync(request);
          break;
        case "suspend":
          promise = suspendMutation.mutateAsync(request);
          break;
        case "resume":
          promise = resumeMutation.mutateAsync(request);
          break;
      }

      void promise
        .catch(() => undefined)
        .finally(() => {
          setPendingActions((current) => {
            if (current.get(vm.name) !== action) return current;
            const next = new Map(current);
            next.delete(vm.name);
            return next;
          });
        });
    },
    [
      forceOffMutation,
      pendingActions,
      rebootMutation,
      resumeMutation,
      shutdownMutation,
      startMutation,
      suspendMutation,
    ],
  );

  return (
    <>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-16)",
          minHeight: 0,
        }}
      >
        <VMPreflightCard preflight={preflight} />
        <div
          style={{
            alignItems: "stretch",
            display: "grid",
            gap: "var(--app-space-16)",
            gridTemplateColumns: isCompactLayout
              ? "1fr"
              : "minmax(0, 1fr) minmax(280px, 360px)",
            minHeight: 0,
          }}
        >
          <VMListTable
            effectiveSelectedName={selectedName}
            onDelete={(vm) => {
              setDeleteTargetName(vm.name);
              setPendingDeleteVM(vm);
              setDeleteDialogOpen(true);
            }}
            onOpenConsole={(vm) => {
              setConsoleSession({
                stream: openVMConsoleStream(vm.name),
                vm,
              });
              setConsoleDialogOpen(true);
            }}
            onRunAction={runAction}
            onSelect={setSelectedName}
            pendingActions={pendingActions}
            vms={vms}
          />
          <Outlet />
        </div>
      </div>

      {deleteTarget && (
        <DeleteVMDialog
          isDeleting={deleteMutation.isPending}
          onClose={() => setDeleteDialogOpen(false)}
          onDelete={(deleteDisks) => {
            deleteMutation.mutate({
              deleteDisks,
              name: deleteTarget.name,
            });
          }}
          onExited={() => {
            setDeleteTargetName(null);
            setPendingDeleteVM(null);
          }}
          open={deleteDialogOpen}
          vm={deleteTarget}
        />
      )}
      <Suspense fallback={null}>
        {consoleSession && (
          <ConsoleDialog
            onClose={() => setConsoleDialogOpen(false)}
            onExited={() => setConsoleSession(null)}
            open={consoleDialogOpen}
            session={consoleSession}
          />
        )}
      </Suspense>
    </>
  );
};

export default VMMachinesLayout;
