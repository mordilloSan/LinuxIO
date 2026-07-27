import { Icon } from "@iconify/react";
import { useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi, Outlet } from "@tanstack/react-router";
import { createContext, useContext, useState, type ReactNode } from "react";

import {
  linuxio,
  type VMCreateProgress,
  type VMPreflight,
  type VirtualMachine,
} from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

import CreateVMDialog from "./CreateVMDialog";
import { VM_TOAST, preflightReady } from "./vmShared";
import { VM_TABS } from "./vmTabs";

export interface VMRouteData {
  preflight?: VMPreflight;
  vms: VirtualMachine[];
}

const VMRouteDataContext = createContext<VMRouteData | null>(null);
const vmRouteApi = getRouteApi("/_authenticated/vm");

export function useVMRouteData(): VMRouteData {
  const value = useContext(VMRouteDataContext);
  if (!value) {
    throw new Error("VM child content must render inside VMPage");
  }
  return value;
}

interface VMPageProps {
  children?: ReactNode;
}

const VMPage = ({ children }: VMPageProps) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const navigate = vmRouteApi.useNavigate();
  const toast = useScopedToast(VM_TOAST);
  const { status: libvirtStatus, reason: libvirtReason } =
    useCapability("libvirtAvailable");
  const [createOpen, setCreateOpen] = useState(false);
  const [createProgress, setCreateProgress] = useState<VMCreateProgress | null>(
    null,
  );
  const [listQuery, preflightQuery] = useSuspenseQueries({
    queries: [
      linuxio.virt.list.queryOptions({
        refetchInterval: 5000,
      }),
      linuxio.virt.preflight.queryOptions(
        {},
        {
          refetchInterval: 15000,
        },
      ),
    ],
  });

  const createMutation = linuxio.virt.create.useJobStreamAction<
    VirtualMachine,
    VMCreateProgress
  >({
    closeMessage:
      "VM create connection closed before final result. Refresh the VM list to check whether creation completed.",
    onProgress: (progress) => setCreateProgress(progress),
    invalidates: (vm) => [
      linuxio.virt.list.queryKey(),
      linuxio.virt.get.queryKey(vm.name),
    ],
    success: (vm) => {
      toast.success(`Created ${vm.name}`);
      setCreateProgress(null);
      setCreateOpen(false);
      navigate({
        search: { vm: vm.name },
        to: "/vm/machines",
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
          message: "Starting VM create job",
          phase: "starting",
        });
      },
    },
  });

  const tabActions = (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        flexWrap: isMobile ? "wrap" : "nowrap",
        gap: theme.spacing(1.5),
      }}
    >
      <AppButton
        disabled={listQuery.isFetching}
        onClick={() => listQuery.refetch()}
        startIcon={
          listQuery.isFetching ? (
            <AppCircularProgress color="inherit" size={16} />
          ) : (
            <Icon height={18} icon="mdi:refresh" width={18} />
          )
        }
        variant="outlined"
      >
        Refresh
      </AppButton>
      <AppButton
        disabled={!preflightReady(preflightQuery.data)}
        onClick={() => {
          setCreateProgress(null);
          setCreateOpen(true);
        }}
        startIcon={<Icon height={18} icon="mdi:plus" width={18} />}
        variant="contained"
      >
        Create VM
      </AppButton>
    </div>
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
    <VMRouteDataContext
      value={{
        preflight: preflightQuery.data,
        vms: listQuery.data,
      }}
    >
      <RoutedTabContainer rightContent={tabActions} tabs={VM_TABS}>
        {children ?? <Outlet />}
      </RoutedTabContainer>

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
    </VMRouteDataContext>
  );
};

export default VMPage;
