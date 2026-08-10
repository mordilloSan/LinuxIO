import { useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi, Outlet } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";

import { linuxio, type VMCreateProgress } from "@/api";
import { RoutedTabActions, RoutedTabLayout } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

import CreateVMDialog from "./CreateVMDialog";
import { VM_TOAST, preflightReady } from "./vmShared";
import { VM_TABS } from "./vmTabs";

const vmRouteApi = getRouteApi("/_authenticated/vm");

interface VMPageProps {
  children?: ReactNode;
}

const VMPage = ({ children }: VMPageProps) => {
  const theme = useAppTheme();
  const navigate = vmRouteApi.useNavigate();
  const toast = useScopedToast(VM_TOAST);
  const { status: libvirtStatus, reason: libvirtReason } =
    useCapability("libvirtAvailable");
  const [createOpen, setCreateOpen] = useState(false);
  const [createProgress, setCreateProgress] = useState<VMCreateProgress | null>(
    null,
  );
  // This layout stays mounted for the whole VMs section, so it owns the poll
  // cadence for both entries. Child routes observe the same keys with no
  // interval of their own and inherit this freshness.
  const [listQuery, preflightQuery] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchInterval: 5000 },
      { ...linuxio.virt.preflight({}), refetchInterval: 15000 },
    ],
  });

  const createMutation = linuxio.virt.create.useTaskStreamAction({
    closeMessage:
      "VM create connection closed before final result. Refresh the VM list to check whether creation completed.",
    onProgress: (progress) => setCreateProgress(progress),
    invalidates: (vm) => [
      linuxio.virt.list.queryKey,
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
    <>
      <AppActionIconButton
        ariaLabel="Refresh"
        disabled={listQuery.isFetching}
        icon="mdi:refresh"
        iconSize={20}
        label="Refresh"
        loading={listQuery.isFetching}
        onClick={() => listQuery.refetch()}
      />
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
    </>
  );

  if (libvirtStatus !== "available") {
    return (
      <div style={{ padding: theme.spacing(3) }}>
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

      {createOpen && (
        <CreateVMDialog
          createProgress={createProgress}
          isCreating={createMutation.isPending}
          onClose={() => {
            setCreateOpen(false);
            setCreateProgress(null);
          }}
          onCreate={(request) => createMutation.mutate(request)}
          open={createOpen}
        />
      )}
    </>
  );
};

export default VMPage;
