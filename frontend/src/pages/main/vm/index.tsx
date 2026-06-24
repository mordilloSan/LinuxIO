import { Icon } from "@iconify/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";

import ConsoleDialog from "./ConsoleDialog";
import CreateVMDialog from "./CreateVMDialog";
import DeleteVMDialog from "./DeleteVMDialog";
import VMDetailsPanel from "./VMDetailsPanel";
import VMListTable from "./VMListTable";
import {
  type ConsoleSession,
  type VMAction,
  VM_TOAST,
  normalizeVMDeleteResult,
  preflightReady,
} from "./vmShared";
import {
  VMDashboardTab,
  VMImagesTab,
  VMNetworksTab,
  VMPreflightCard,
} from "./VMTabs";

import {
  isJobSnapshot,
  LinuxIOError,
  linuxio,
  openJobAttachStream,
  openVMConsoleStream,
  waitForStreamResult,
} from "@/api";
import type {
  JobSnapshot,
  VMCreateRequest,
  VMCreateProgress,
  VMDeleteRequest,
  VMDeleteResult,
  VirtualMachine,
} from "@/api";
import { TabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useTabUrlState } from "@/hooks/useTabUrlState";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

async function expectJobSnapshot(
  started: Promise<unknown>,
): Promise<JobSnapshot> {
  const job = await started;
  if (isJobSnapshot(job)) {
    return job;
  }
  throw new LinuxIOError("VM job did not return a job snapshot", "invalid_job");
}

const Page: React.FC = () => {
  const theme = useAppTheme();
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const queryClient = useQueryClient();
  const toast = useScopedToast(VM_TOAST);
  const { status: libvirtStatus, reason: libvirtReason } =
    useCapability("libvirtAvailable");
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [createProgress, setCreateProgress] = useState<VMCreateProgress | null>(
    null,
  );
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [consoleSession, setConsoleSession] = useState<ConsoleSession | null>(
    null,
  );
  const [, setActiveTab] = useTabUrlState("dashboard", "vmTab");

  const listQuery = linuxio.virt.list.useQuery({
    enabled: libvirtStatus === "available",
    refetchInterval: libvirtStatus === "available" ? 5000 : false,
  });
  const preflightQuery = linuxio.virt.preflight.useQuery(
    {},
    {
      enabled: libvirtStatus === "available",
      refetchInterval: libvirtStatus === "available" ? 15000 : false,
    },
  );
  const vms = useMemo(() => listQuery.data ?? [], [listQuery.data]);
  const effectiveSelectedName = useMemo(() => {
    if (selectedName && vms.some((vm) => vm.name === selectedName)) {
      return selectedName;
    }
    return vms[0]?.name ?? null;
  }, [selectedName, vms]);
  const detailQuery = linuxio.virt.get.useQuery(effectiveSelectedName ?? "", {
    enabled: libvirtStatus === "available" && Boolean(effectiveSelectedName),
  });
  const selectedVM =
    detailQuery.data ??
    vms.find((vm) => vm.name === effectiveSelectedName) ??
    null;

  const invalidateVMs = useCallback(
    (name: string | null = effectiveSelectedName) => {
      queryClient.invalidateQueries({ queryKey: linuxio.virt.list.queryKey() });
      if (name) {
        queryClient.invalidateQueries({
          queryKey: linuxio.virt.get.queryKey(name),
        });
      }
    },
    [effectiveSelectedName, queryClient],
  );

  const mutationOptions = useCallback(
    (successText: string, fallback: string) => ({
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, fallback)),
      onSuccess: () => {
        toast.success(successText);
        invalidateVMs();
      },
    }),
    [invalidateVMs, toast],
  );

  const createMutation = useMutation<VirtualMachine, Error, VMCreateRequest>({
    mutationFn: async (request) => {
      setCreateProgress({
        message: "Starting VM create job",
        phase: "starting",
      });
      const job = await expectJobSnapshot(linuxio.virt.create(request));
      return await waitForStreamResult<VirtualMachine, VMCreateProgress>(
        openJobAttachStream(job.id),
        {
          closeMessage:
            "VM create connection closed before final result. Refresh the VM list to check whether creation completed.",
          onProgress: (progress) => setCreateProgress(progress),
        },
      );
    },
    onError: (err: Error) => {
      const message = getMutationErrorMessage(err, "Failed to create VM");
      setCreateProgress({
        message,
        phase: "error",
      });
      toast.error(message);
    },
    onSuccess: (vm) => {
      toast.success(`Created ${vm.name}`);
      setCreateProgress(null);
      setCreateOpen(false);
      setSelectedName(vm.name);
      setActiveTab("machines");
      invalidateVMs(vm.name);
    },
  });
  const deleteMutation = useMutation<VMDeleteResult, Error, VMDeleteRequest>({
    mutationFn: async (request) => {
      const job = await expectJobSnapshot(linuxio.virt.delete(request));
      return await waitForStreamResult<VMDeleteResult>(
        openJobAttachStream(job.id),
        {
          closeMessage:
            "VM delete connection closed before final result. Refresh the VM list to check whether deletion completed.",
        },
      );
    },
    onError: (err: Error) =>
      toast.error(getMutationErrorMessage(err, "Failed to delete VM")),
    onSuccess: (result: VMDeleteResult, request: VMDeleteRequest) => {
      const deleteResult = normalizeVMDeleteResult(result);
      const diskText =
        deleteResult.removed.length > 0
          ? ` Removed ${deleteResult.removed.length} disk(s).`
          : "";
      toast.success(`Deleted ${request.name}.${diskText}`);
      setDeleteOpen(false);
      setSelectedName(null);
      // The domain is gone. Optimistically drop it from the cached list so the
      // detail query stops targeting it. Invalidating virt.get for the deleted
      // VM (as invalidateVMs does) would refetch a missing domain and surface a
      // spurious "domain not found" error toast.
      queryClient.setQueryData<VirtualMachine[]>(
        linuxio.virt.list.queryKey(),
        (current) => current?.filter((vm) => vm.name !== request.name),
      );
      queryClient.invalidateQueries({
        queryKey: linuxio.virt.list.queryKey(),
      });
    },
  });
  const startMutation = linuxio.virt.start.useMutation(
    mutationOptions("VM started", "Failed to start VM"),
  );
  const shutdownMutation = linuxio.virt.shutdown.useMutation(
    mutationOptions("VM shutdown requested", "Failed to shutdown VM"),
  );
  const rebootMutation = linuxio.virt.reboot.useMutation(
    mutationOptions("VM reboot requested", "Failed to reboot VM"),
  );
  const forceOffMutation = linuxio.virt.force_off.useMutation(
    mutationOptions("VM powered off", "Failed to force off VM"),
  );
  const suspendMutation = linuxio.virt.suspend.useMutation(
    mutationOptions("VM suspended", "Failed to suspend VM"),
  );
  const resumeMutation = linuxio.virt.resume.useMutation(
    mutationOptions("VM resumed", "Failed to resume VM"),
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
        onClick={() => listQuery.refetch()}
        startIcon={<Icon height={18} icon="mdi:refresh" width={18} />}
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
    <>
      <TabContainer
        defaultTab="dashboard"
        tabs={[
          {
            component: (
              <VMDashboardTab preflight={preflightQuery.data} vms={vms} />
            ),
            label: "Global dashboard",
            rightContent: tabActions,
            value: "dashboard",
          },
          {
            component: <VMNetworksTab vms={vms} />,
            label: "Networks",
            rightContent: tabActions,
            value: "networks",
          },
          {
            component: <VMImagesTab preflight={preflightQuery.data} />,
            label: "Images",
            rightContent: tabActions,
            value: "images",
          },
          {
            component: (
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: theme.spacing(4.5),
                  minHeight: 0,
                }}
              >
                <VMPreflightCard preflight={preflightQuery.data} />
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
                    isLoading={listQuery.isLoading}
                    onDelete={(vm) => {
                      setSelectedName(vm.name);
                      setDeleteOpen(true);
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
                  <VMDetailsPanel vm={selectedVM} />
                </div>
              </div>
            ),
            label: "Virtual machines",
            rightContent: tabActions,
            value: "machines",
          },
        ]}
        urlParam="vmTab"
      />

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
      {deleteOpen && (
        <DeleteVMDialog
          isDeleting={deleteMutation.isPending}
          onClose={() => setDeleteOpen(false)}
          onDelete={(deleteDisks) => {
            if (selectedVM) {
              deleteMutation.mutate({ deleteDisks, name: selectedVM.name });
            }
          }}
          open={deleteOpen}
          vm={selectedVM}
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

export default Page;
