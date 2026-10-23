import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi, Outlet, useParams } from "@tanstack/react-router";
import { lazy, Suspense, useCallback, useState } from "react";

import {
  getMutationErrorMessage,
  linuxio,
  useCallMutation,
  type VirtualMachine,
} from "@/api";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { RoutedTabActions } from "@/components/tabbar";
import AppTypography from "@/components/ui/AppTypography";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useClearSelectionOnEscape } from "@/hooks/useClearSelectionOnEscape";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";
import { CARD_GRID_SIZE_DENSE } from "@/theme/constants";

import DeleteVMDialog from "./DeleteVMDialog";
import EditVMDialog from "./EditVMDialog";
import { useVMLifecycle } from "./useVMLifecycle";
import VMActions, { type VMActionHandlers } from "./VMActions";
import VMCard from "./VMCard";
import VMListTable from "./VMListTable";
import { VM_TOAST, normalizeVMDeleteResult } from "./vmShared";
import { VMPreflightCard } from "./VMTabs";

// noVNC only loads once a console is opened, like the docker TerminalDialog.
const ConsoleDialog = lazy(() => import("./ConsoleDialog"));

const vmMachinesRouteApi = getRouteApi("/_authenticated/vm/machines");

const getVirtualMachineId = (vm: VirtualMachine) => vm.name;

/**
 * Persistent shell for the Virtual machines tab.
 *
 * The list and every lifecycle mutation live here so they survive detail
 * navigation; the selected machine is a real child route rendered in the
 * outlet. Selection therefore lives in the path, not in search state, and
 * replaces the table or card list with the focused card plus its details.
 */
const VMMachinesLayout = () => {
  const queryClient = useQueryClient();
  const [viewMode, setViewMode] = useViewMode("vm.machines");
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
  // A snapshot: the form copies it once per open, and a rename drops the
  // original row from the polled list.
  const [editVM, setEditVM] = useState<VirtualMachine | null>(null);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [consoleVM, setConsoleVM] = useState<VirtualMachine | null>(null);
  const [consoleDialogOpen, setConsoleDialogOpen] = useState(false);

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

  const deleteMutation = useCallMutation(linuxio.virt.delete, {
    // Disks kept by a delete show up on the Disks tab.
    invalidates: [
      linuxio.virt.list.queryKey,
      linuxio.virt.unused_disks.queryKey,
    ],
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
  const { pendingActions, runAction } = useVMLifecycle(vms);
  const surface = useReorderableSurface({
    getId: getVirtualMachineId,
    items: vms,
    surface: "vm.list",
  });
  const selectedVM = vms.find((vm) => vm.name === selectedName);
  useClearSelectionOnEscape(selectedName !== null, () => setSelectedName(null));
  const handlers: VMActionHandlers = {
    onDelete: (vm) => {
      setDeleteTargetName(vm.name);
      setPendingDeleteVM(vm);
      setDeleteDialogOpen(true);
    },
    onEdit: (vm) => {
      setEditVM(vm);
      setEditDialogOpen(true);
    },
    onOpenConsole: (vm) => {
      setConsoleVM(vm);
      setConsoleDialogOpen(true);
    },
    onRunAction: runAction,
  };

  let content;
  if (selectedVM) {
    content = (
      <ResourceDetailsLayout
        onClose={() => void setSelectedName(null)}
        resourceLabel="VM"
        subtitle="Live metrics, settings, disks and network"
        summary={
          <VMCard
            actions={
              <VMActions
                handlers={handlers}
                pendingAction={pendingActions.get(selectedVM.name)}
                vm={selectedVM}
              />
            }
            onSelect={() => void setSelectedName(null)}
            pendingAction={pendingActions.get(selectedVM.name)}
            selected
            vm={selectedVM}
          />
        }
        title={selectedVM.name}
      >
        <Outlet />
      </ResourceDetailsLayout>
    );
  } else if (selectedName) {
    // Not in the polled list yet (just created or renamed): the route still
    // shows its details.
    content = <Outlet />;
  } else if (viewMode === "table") {
    content = (
      <VMListTable
        handlers={handlers}
        onSelect={setSelectedName}
        pendingActions={pendingActions}
        surface={surface}
      />
    );
  } else if (vms.length === 0) {
    content = (
      <AppTypography color="text.secondary" variant="body2">
        No virtual machines.
      </AppTypography>
    );
  } else {
    content = (
      <ReorderableCardGrid
        fillAvailable
        getId={getVirtualMachineId}
        items={surface.items}
        renderItem={(vm) => (
          <VMCard
            actions={
              <VMActions
                compact
                handlers={handlers}
                pendingAction={pendingActions.get(vm.name)}
                vm={vm}
              />
            }
            onSelect={() => void setSelectedName(vm.name)}
            pendingAction={pendingActions.get(vm.name)}
            vm={vm}
          />
        )}
        size={CARD_GRID_SIZE_DENSE}
        surface={surface}
      />
    );
  }

  return (
    <>
      <RoutedTabActions>
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setViewMode}
          viewMode={viewMode}
        />
      </RoutedTabActions>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-16)",
          minHeight: 0,
        }}
      >
        <VMPreflightCard preflight={preflight} />
        {content}
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
      {editVM && (
        <EditVMDialog
          onClose={() => setEditDialogOpen(false)}
          open={editDialogOpen}
          vm={editVM}
        />
      )}
      <Suspense fallback={null}>
        {consoleVM && (
          <ConsoleDialog
            onClose={() => setConsoleDialogOpen(false)}
            onExited={() => setConsoleVM(null)}
            open={consoleDialogOpen}
            vm={consoleVM}
          />
        )}
      </Suspense>
    </>
  );
};

export default VMMachinesLayout;
