import { useQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState } from "react";

import {
  linuxio,
  openVMConsoleStream,
  type VMDeleteResult,
  type VirtualMachine,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

import ConsoleDialog from "./ConsoleDialog";
import DeleteVMDialog from "./DeleteVMDialog";
import VMDetailsPanel from "./VMDetailsPanel";
import VMListTable from "./VMListTable";
import { useVMRouteData } from "./VMPage";
import {
  type ConsoleSession,
  type VMAction,
  VM_TOAST,
  normalizeVMDeleteResult,
} from "./vmShared";
import { VMPreflightCard } from "./VMTabs";

const vmMachinesRouteApi = getRouteApi("/_authenticated/vm/machines");

const VMMachinesPage = () => {
  const theme = useAppTheme();
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const vmListCache = linuxio.virt.list.useCache();
  const toast = useScopedToast(VM_TOAST);
  const search = vmMachinesRouteApi.useSearch();
  const navigate = vmMachinesRouteApi.useNavigate();
  const { preflight, vms } = useVMRouteData();
  // Keep the delete subject separate from the URL-driven detail selection.
  // Selecting a row updates search asynchronously, so deriving this from
  // selectedVM could delete the previously selected machine.
  const [deleteTargetName, setDeleteTargetName] = useState<string | null>(null);
  // Deleting undefines the domain before disk cleanup finishes, so the polled
  // list can drop the row mid-job; this snapshot keeps the dialog mounted
  // until the mutation settles.
  const [pendingDeleteVM, setPendingDeleteVM] = useState<VirtualMachine | null>(
    null,
  );
  const [consoleSession, setConsoleSession] = useState<ConsoleSession | null>(
    null,
  );

  const effectiveSelectedName = useMemo(() => {
    if (search.vm && vms.some((vm) => vm.name === search.vm)) {
      return search.vm;
    }
    return vms[0]?.name ?? null;
  }, [search.vm, vms]);
  const setSelectedName = useCallback(
    (vm: string | null) =>
      navigate({
        replace: true,
        search: (previous) => ({
          ...previous,
          vm: vm ?? undefined,
        }),
        to: "/vm/machines",
      }),
    [navigate],
  );
  const detailQuery = useQuery(
    linuxio.virt.get.queryOptions(effectiveSelectedName ?? "", {
      enabled: Boolean(effectiveSelectedName),
    }),
  );
  const selectedVM =
    detailQuery.data ??
    vms.find((vm) => vm.name === effectiveSelectedName) ??
    null;
  const liveDeleteTarget =
    vms.find((vm) => vm.name === deleteTargetName) ?? null;

  const actionConfig = (successText: string, fallback: string) => ({
    success: successText,
    error: fallback,
    toast: VM_TOAST,
  });
  const deleteMutation = linuxio.virt.delete.useJobStreamAction<VMDeleteResult>(
    {
      closeMessage:
        "VM delete connection closed before final result. Refresh the VM list to check whether deletion completed.",
      invalidates: [linuxio.virt.list.queryKey()],
      success: (result, request) => {
        const deleteResult = normalizeVMDeleteResult(result);
        const diskText =
          deleteResult.removed.length > 0
            ? ` Removed ${deleteResult.removed.length} disk(s).`
            : "";
        toast.success(`Deleted ${request.name}.${diskText}`);
        setDeleteTargetName(null);
        setPendingDeleteVM(null);
        vmListCache.set((current) =>
          current?.filter((vm) => vm.name !== request.name),
        );
        setSelectedName(null);
      },
      error: (error) => {
        toast.error(getMutationErrorMessage(error, "Failed to delete VM"));
        setPendingDeleteVM(null);
      },
    },
  );
  const deleteTarget =
    liveDeleteTarget ?? (deleteMutation.isPending ? pendingDeleteVM : null);
  const startMutation = linuxio.virt.start.useJobAction(
    actionConfig("VM started", "Failed to start VM"),
  );
  const shutdownMutation = linuxio.virt.shutdown.useJobAction(
    actionConfig("VM shutdown requested", "Failed to shutdown VM"),
  );
  const rebootMutation = linuxio.virt.reboot.useJobAction(
    actionConfig("VM reboot requested", "Failed to reboot VM"),
  );
  const forceOffMutation = linuxio.virt.force_off.useJobAction(
    actionConfig("VM powered off", "Failed to force off VM"),
  );
  const suspendMutation = linuxio.virt.suspend.useJobAction(
    actionConfig("VM suspended", "Failed to suspend VM"),
  );
  const resumeMutation = linuxio.virt.resume.useJobAction(
    actionConfig("VM resumed", "Failed to resume VM"),
  );
  const actionPending =
    startMutation.isPending ||
    shutdownMutation.isPending ||
    rebootMutation.isPending ||
    forceOffMutation.isPending ||
    suspendMutation.isPending ||
    resumeMutation.isPending;

  const runAction = useCallback(
    (action: VMAction, vm: VirtualMachine) => {
      const request = { name: vm.name };
      switch (action) {
        case "start":
          startMutation.mutate(request);
          break;
        case "shutdown":
          shutdownMutation.mutate(request);
          break;
        case "reboot":
          rebootMutation.mutate(request);
          break;
        case "force_off":
          forceOffMutation.mutate(request);
          break;
        case "suspend":
          suspendMutation.mutate(request);
          break;
        case "resume":
          resumeMutation.mutate(request);
          break;
      }
    },
    [
      forceOffMutation,
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
          gap: theme.spacing(4.5),
          minHeight: 0,
        }}
      >
        <VMPreflightCard preflight={preflight} />
        <div
          style={{
            alignItems: "stretch",
            display: "grid",
            gap: theme.spacing(4.5),
            gridTemplateColumns: isCompactLayout
              ? "1fr"
              : "minmax(0, 1fr) minmax(280px, 360px)",
            minHeight: 0,
          }}
        >
          <VMListTable
            actionPending={actionPending}
            effectiveSelectedName={effectiveSelectedName}
            onDelete={(vm) => {
              setDeleteTargetName(vm.name);
            }}
            onOpenConsole={(vm) =>
              setConsoleSession({
                stream: openVMConsoleStream(vm.name),
                vm,
              })
            }
            onRunAction={runAction}
            onSelect={setSelectedName}
            vms={vms}
          />
          <VMDetailsPanel
            error={
              detailQuery.isLoadingError
                ? detailQuery.error?.message
                : undefined
            }
            isLoading={detailQuery.isLoading}
            vm={selectedVM}
          />
        </div>
      </div>

      {deleteTarget && (
        <DeleteVMDialog
          isDeleting={deleteMutation.isPending}
          onClose={() => {
            setDeleteTargetName(null);
            setPendingDeleteVM(null);
          }}
          onDelete={(deleteDisks) => {
            setPendingDeleteVM(deleteTarget);
            deleteMutation.mutate({ deleteDisks, name: deleteTarget.name });
          }}
          open
          vm={deleteTarget}
        />
      )}
      {consoleSession && (
        <ConsoleDialog
          onClose={() => setConsoleSession(null)}
          open={Boolean(consoleSession)}
          session={consoleSession}
        />
      )}
    </>
  );
};

export default VMMachinesPage;
