import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi, Outlet, useMatchRoute } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";

import {
  getMutationErrorMessage,
  linuxio,
  type VMCreateProgress,
  type VMUnusedDisk,
} from "@/api";
import { RoutedTabActions, RoutedTabLayout } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";

import CreateVMDialog from "./CreateVMDialog";
import { VMDisksPruneButton, VMDisksPruneDialog } from "./VMDisksPrune";
import { VM_TOAST, preflightReady } from "./vmShared";
import { VM_TABS } from "./vmTabs";

const vmRouteApi = getRouteApi("/_authenticated/vm");

interface VMPageProps {
  children?: ReactNode;
}

const VMPage = ({ children }: VMPageProps) => {
  const navigate = vmRouteApi.useNavigate();
  const search = vmRouteApi.useSearch();
  const matchRoute = useMatchRoute();
  // The layout owns the header so the Disks tab's broom sits in the shared
  // slot order, left of Refresh and Create; a child's RoutedTabActions would
  // append it after them.
  const onDisksTab = Boolean(matchRoute({ to: "/vm/disks" }));
  // The disks the broom showed when pressed are the ones the prune deletes.
  const [prune, setPrune] = useState<{ disks: VMUnusedDisk[]; open: boolean }>({
    disks: [],
    open: false,
  });
  // Leaving the tab (a Back press) closes the prune instead of keeping it for
  // the return.
  if (prune.open && !onDisksTab) setPrune({ ...prune, open: false });
  const queryClient = useQueryClient();
  const toast = useScopedToast(VM_TOAST);
  const { status: libvirtStatus, reason: libvirtReason } =
    useCapability("libvirtAvailable");
  const [createOpen, setCreateOpen] = useState(false);
  const [createProgress, setCreateProgress] = useState<VMCreateProgress | null>(
    null,
  );
  // This layout stays mounted for the whole VMs section, so it owns the poll
  // cadence for shared VM data. Child routes observe the same keys with no
  // interval of their own and inherit this freshness. The create dialog owns
  // the available-network query because nothing else consumes it.
  const [listQuery, preflightQuery] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchInterval: 5000 },
      { ...linuxio.virt.preflight({}), refetchInterval: 15000 },
    ],
  });

  const createMutation = linuxio.virt.create.useMutation({
    onProgress: (progress) => {
      setCreateProgress(
        progress.detail ?? {
          message: progress.message ?? "Creating virtual machine",
          phase: progress.phase ?? "running",
          percent: progress.percentage,
        },
      );
    },
    invalidates: (vm) => [
      linuxio.virt.list.queryKey,
      linuxio.virt.templates.queryKey,
      linuxio.virt.get({ name: vm.name }).queryKey,
    ],
    success: (vm) => {
      toast.success(`Created ${vm.name}`);
      setCreateProgress(null);
      setCreateOpen(false);
      void navigate({
        params: { name: vm.name },
        to: "/vm/machines/$name",
      });
    },
    error: (error) => {
      const message = getMutationErrorMessage(error, "Failed to create VM");
      setCreateProgress({
        message,
        phase: "error",
      });
      toast.error(message);
    },
    options: {
      onMutate: () => {
        setCreateProgress({
          message: "Starting VM create task",
          phase: "starting",
        });
      },
    },
  });

  const tabActions = (
    <HeaderActions
      maintenance={
        onDisksTab && (
          <VMDisksPruneButton
            onClick={(disks) => setPrune({ disks, open: true })}
          />
        )
      }
      create={
        <AppActionIconButton
          ariaLabel="Create VM"
          disabled={!preflightReady(preflightQuery.data)}
          icon="mdi:plus"
          iconSize={20}
          label="Create VM"
          onClick={() => {
            setCreateProgress(null);
            setCreateOpen(true);
          }}
        />
      }
      refresh={
        <AppActionIconButton
          ariaLabel="Refresh"
          disabled={listQuery.isFetching}
          icon="mdi:refresh"
          iconSize={20}
          label="Refresh"
          loading={listQuery.isFetching}
          onClick={() => {
            void listQuery.refetch();
            // Only active, so this reloads templates while that tab is open.
            void queryClient.refetchQueries({
              queryKey: linuxio.virt.templates.queryKey,
            });
            // The Disks tab's pools and unused disks, while it is open.
            for (const queryKey of [
              linuxio.virt.pools.queryKey,
              linuxio.virt.unused_disks.queryKey,
            ]) {
              void queryClient.refetchQueries({ queryKey, type: "active" });
            }
          }}
        />
      }
    />
  );

  if (libvirtStatus !== "available") {
    return (
      <div style={{ padding: "var(--app-space-12)" }}>
        <AppAlert severity={libvirtStatus === "unknown" ? "info" : "warning"}>
          <AppAlertTitle>
            {libvirtStatus === "unknown"
              ? "Checking libvirt"
              : "libvirt unavailable"}
          </AppAlertTitle>
          <AppTypography variant="body2">{libvirtReason}</AppTypography>
        </AppAlert>
      </div>
    );
  }

  return (
    <>
      <RoutedTabLayout tabs={VM_TABS}>
        <RoutedTabActions>{tabActions}</RoutedTabActions>
        {children ?? <Outlet />}
      </RoutedTabLayout>

      <CreateVMDialog
        createProgress={createProgress}
        handoffOperationId={search.handoffOperationId ?? ""}
        isCreating={createMutation.isPending}
        onClose={() => {
          setCreateOpen(false);
          setCreateProgress(null);
        }}
        onCreate={(request) => createMutation.mutate(request)}
        onHandoffOperationIdChange={(operationId) => {
          setCreateOpen(true);
          return navigate({
            search: (previous) => ({
              ...previous,
              handoffOperationId: operationId,
            }),
            replace: true,
          });
        }}
        open={createOpen || Boolean(search.handoffOperationId)}
      />
      <VMDisksPruneDialog
        disks={prune.disks}
        onClose={() => setPrune((current) => ({ ...current, open: false }))}
        open={prune.open}
      />
    </>
  );
};

export default VMPage;
