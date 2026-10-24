import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, useParams } from "@tanstack/react-router";
import { useState, type CSSProperties, type SyntheticEvent } from "react";

import { getMutationErrorMessage, linuxio, useCallMutation } from "@/api";
import type { VirtualMachine, VMFirmware, VMUpdateRequest } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { useScopedToast } from "@/hooks/useScopedToast";
import { down } from "@/theme/breakpoints";

import { VM_OS_OPTIONS, VM_TOAST } from "./vmShared";

const vmMachinesRouteApi = getRouteApi("/_authenticated/vm/machines");

const formGridStyle = (isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: "var(--app-dialog-field-gap)",
  gridTemplateColumns: isMobile ? "1fr" : "repeat(2, minmax(0, 1fr))",
});

function EditVMForm({
  onClose,
  vm,
}: {
  onClose: () => void;
  vm: VirtualMachine;
}) {
  const isMobile = useMediaQuery(down("sm"));
  const queryClient = useQueryClient();
  const navigate = vmMachinesRouteApi.useNavigate();
  const selectedName = useParams({
    from: "/_authenticated/vm/machines/$name",
    shouldThrow: false,
  })?.name;
  const toast = useScopedToast(VM_TOAST);
  const currentNetwork = vm.nics?.[0]?.network ?? "";
  const [name, setName] = useState(vm.name);
  const [vcpus, setVCPUs] = useState(String(vm.vcpus));
  const [memoryMB, setMemoryMB] = useState(String(vm.memoryMB));
  const [diskGB, setDiskGB] = useState(String(vm.diskGB));
  const [firmware, setFirmware] = useState<VMFirmware>(vm.firmware ?? "bios");
  const [network, setNetwork] = useState(currentNetwork);
  const currentOS = vm.os ?? "";
  const [os, setOS] = useState(currentOS);
  const networksQuery = useQuery(linuxio.virt.networks);
  const hostBridges = (networksQuery.data ?? []).filter(
    (candidate) => candidate.type === "bridge",
  );

  const updateMutation = useCallMutation(linuxio.virt.update, {
    // Invalidating virt.get would refetch a renamed VM under its old name
    // while that route is still mounted, so the cache takes the result.
    invalidates: () => [],
    success: (result, request) => {
      queryClient.setQueryData(
        linuxio.virt.get({ name: result.name }).queryKey,
        result,
      );
      queryClient.setQueryData<VirtualMachine[]>(
        linuxio.virt.list.queryKey,
        (current) =>
          current?.map((item) => (item.name === request.name ? result : item)),
      );
      const removeOldName = () =>
        queryClient.removeQueries({
          queryKey: linuxio.virt.get({ name: request.name }).queryKey,
        });
      if (result.name !== request.name && selectedName === request.name) {
        void navigate({
          params: { name: result.name },
          replace: true,
          to: "/vm/machines/$name",
        }).then(removeOldName);
      } else if (result.name !== request.name) {
        removeOldName();
      }
      toast.success(`Updated ${result.name}`);
      onClose();
    },
    error: (error) => {
      toast.error(getMutationErrorMessage(error, "Failed to update VM"));
    },
  });

  const parsedVCPUs = Number.parseInt(vcpus, 10);
  const parsedMemoryMB = Number.parseInt(memoryMB, 10);
  const parsedDiskGB = Number.parseInt(diskGB, 10);
  const nameValid = /^[A-Za-z0-9_.-]+$/.test(name);
  // vm.diskGB counts only LinuxIO-managed disks, the ones the bridge resizes.
  const diskResizable = vm.diskGB > 0;
  const diskValid = !diskResizable || parsedDiskGB >= vm.diskGB;
  const isBusy = updateMutation.isPending;
  const canSubmit =
    !isBusy &&
    nameValid &&
    parsedVCPUs > 0 &&
    parsedMemoryMB >= 256 &&
    diskValid;

  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) return;
    const request: VMUpdateRequest = {
      firmware,
      memoryMB: parsedMemoryMB,
      name: vm.name,
      vcpus: parsedVCPUs,
    };
    if (name !== vm.name) request.newName = name;
    if (diskResizable && parsedDiskGB !== vm.diskGB) {
      request.diskGB = parsedDiskGB;
    }
    if (network !== currentNetwork) request.network = network;
    if (os !== currentOS) request.os = os;
    updateMutation.mutate(request);
  };

  return (
    <form className="app-dialog-form" onSubmit={handleSubmit}>
      <AppDialogTitle>Edit VM</AppDialogTitle>
      <AppDialogContent>
        <div style={formGridStyle(isMobile)}>
          <AppTextField
            autoFocus
            error={!nameValid}
            fullWidth
            helperText="Letters, numbers, dash, underscore, and dot"
            id="vm-edit-name"
            label="Name"
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
          <AppTextField
            fullWidth
            id="vm-edit-vcpus"
            label="vCPUs"
            onChange={(event) => setVCPUs(event.target.value)}
            required
            type="number"
            value={vcpus}
          />
          <AppTextField
            fullWidth
            id="vm-edit-memory"
            label="Memory MB"
            onChange={(event) => setMemoryMB(event.target.value)}
            required
            type="number"
            value={memoryMB}
          />
          <AppTextField
            disabled={!diskResizable}
            error={!diskValid}
            fullWidth
            helperText={
              diskResizable
                ? "Disks can only grow; extend the guest partition afterwards"
                : "Only LinuxIO-managed disks can be resized"
            }
            id="vm-edit-disk"
            label="Disk GB"
            onChange={(event) => setDiskGB(event.target.value)}
            type="number"
            value={diskGB}
          />
          <AppSelect
            fullWidth
            label="Firmware"
            onChange={(event) => setFirmware(event.target.value as VMFirmware)}
            value={firmware}
          >
            <option value="uefi">UEFI</option>
            <option value="bios">BIOS (legacy)</option>
          </AppSelect>
          <AppSelect
            fullWidth
            label="Operating system"
            onChange={(event) => setOS(event.target.value)}
            value={os}
          >
            {currentOS ? null : <option value="">Unknown</option>}
            {VM_OS_OPTIONS.map((option) => (
              <option key={option.id} value={option.id}>
                {option.label}
              </option>
            ))}
            {currentOS &&
            !VM_OS_OPTIONS.some((option) => option.id === currentOS) ? (
              <option value={currentOS}>{currentOS}</option>
            ) : null}
          </AppSelect>
          {currentNetwork ? (
            <AppSelect
              disabled={networksQuery.isPending}
              fullWidth
              label="Network"
              onChange={(event) => setNetwork(event.target.value)}
              value={network}
            >
              <option value="default">NAT (default)</option>
              {hostBridges.map((bridge) => (
                <option
                  disabled={!bridge.active}
                  key={bridge.name}
                  value={bridge.name}
                >
                  {bridge.name}
                  {!bridge.active ? " (inactive)" : ""}
                </option>
              ))}
              {currentNetwork !== "default" &&
              !hostBridges.some((bridge) => bridge.name === currentNetwork) ? (
                <option value={currentNetwork}>{currentNetwork}</option>
              ) : null}
            </AppSelect>
          ) : null}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isBusy} onClick={onClose} variant="text">
          Cancel
        </AppButton>
        <AppButton
          disabled={!canSubmit}
          startIcon={
            isBusy ? (
              <AppCircularProgress color="inherit" size={16} />
            ) : (
              <Icon height={18} icon="mdi:content-save" width={18} />
            )
          }
          type="submit"
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </form>
  );
}

/** Edits a shut-off VM; the form remounts with the VM's values on each open. */
export default function EditVMDialog({
  onClose,
  open,
  vm,
}: {
  onClose: () => void;
  open: boolean;
  vm: VirtualMachine;
}) {
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <EditVMForm onClose={onClose} vm={vm} />
    </GeneralDialog>
  );
}
