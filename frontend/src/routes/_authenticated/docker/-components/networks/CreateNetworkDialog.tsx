import { useState, type SyntheticEvent } from "react";

import { linuxio, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import SectionHeader from "@/components/ui/SectionHeader";
import { useScopedToast } from "@/hooks/useScopedToast";

import { parseDockerKeyValueLines } from "../shared/dockerKeyValues";
import { DOCKER_RESOURCE_NAME } from "../shared/dockerResourceName";
import { DOCKER_TOAST_META } from "../shared/dockerToast";

interface CreateNetworkDialogProps {
  existingNames: string[];
  onClose: () => void;
  open: boolean;
}

export const CreateNetworkDialog = ({
  open,
  onClose,
  existingNames,
}: CreateNetworkDialogProps) => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const [networkName, setNetworkName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [internal, setInternal] = useState(false);
  const [attachable, setAttachable] = useState(false);
  const [enableIpv6, setEnableIpv6] = useState(false);
  const [subnet, setSubnet] = useState("");
  const [gateway, setGateway] = useState("");
  const [optionsText, setOptionsText] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [formError, setFormError] = useState<string>();
  const parsedOptions = parseDockerKeyValueLines(optionsText);
  const trimmedName = networkName.trim();

  const { mutate: createNetwork, isPending: isCreating } = useCallMutation(
    linuxio.docker.create_network,
    {
      success: () => {
        toast.success(`Network "${trimmedName}" created`);
        handleClose();
      },
      error: "Failed to create network",
      toast: DOCKER_TOAST_META,
    },
  );

  const nameTaken = existingNames.includes(trimmedName);
  const isValidName = DOCKER_RESOURCE_NAME.test(trimmedName);

  const handleCreate = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!trimmedName || !driver.trim()) {
      setFormError("Name and driver are required.");
      return;
    }
    if (nameTaken) {
      setFormError("Choose a network name that is not already in use.");
      return;
    }
    if (!isValidName) {
      setFormError(
        "Name must start with an alphanumeric character and use only letters, numbers, underscores, periods, or hyphens.",
      );
      return;
    }
    if (gateway.trim() && !subnet.trim()) {
      setFormError("Enter a subnet when a gateway is set.");
      return;
    }
    if (parsedOptions.error) {
      setFormError(parsedOptions.error);
      return;
    }
    setFormError(undefined);
    createNetwork({
      name: trimmedName,
      driver: driver.trim(),
      internal,
      attachable,
      enableIpv6,
      subnet: subnet.trim() || undefined,
      gateway: gateway.trim() || undefined,
      options:
        Object.keys(parsedOptions.values).length > 0
          ? parsedOptions.values
          : undefined,
    });
  };

  const handleClose = () => {
    setNetworkName("");
    setDriver("bridge");
    setInternal(false);
    setAttachable(false);
    setEnableIpv6(false);
    setSubnet("");
    setGateway("");
    setOptionsText("");
    setAdvanced(false);
    setFormError(undefined);
    onClose();
  };

  return (
    <GeneralDialog
      aria-label="Create network"
      fullWidth
      maxWidth="xs"
      onClose={isCreating ? undefined : handleClose}
      open={open}
    >
      <form className="app-dialog-form" onSubmit={handleCreate}>
        <AppDialogTitle>Create Network</AppDialogTitle>
        <AppDialogContent>
          <div className="app-dialog-fields">
            <AppTextField
              autoFocus
              disabled={isCreating}
              error={nameTaken || (networkName.length > 0 && !isValidName)}
              fullWidth
              helperText={
                nameTaken
                  ? "This network name already exists."
                  : networkName.length > 0 && !isValidName
                    ? "Use letters, numbers, underscores, periods, or hyphens."
                    : undefined
              }
              label="Name"
              name="network-name"
              onChange={(event) => setNetworkName(event.target.value)}
              required
              value={networkName}
            />
            <AppSelect
              disabled={isCreating}
              fullWidth
              label="Driver"
              onChange={(event) => setDriver(event.target.value)}
              value={driver}
            >
              <option value="bridge">bridge</option>
              <option value="host">host</option>
              <option value="overlay">overlay</option>
              <option value="macvlan">macvlan</option>
              <option value="none">none</option>
            </AppSelect>
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={internal}
                  disabled={isCreating}
                  onChange={(event) => setInternal(event.target.checked)}
                />
              }
              label="Internal network (no external connectivity)"
            />
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={attachable}
                  disabled={isCreating}
                  onChange={(event) => setAttachable(event.target.checked)}
                />
              }
              label="Allow manual container attachment"
            />
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={enableIpv6}
                  disabled={isCreating}
                  onChange={(event) => setEnableIpv6(event.target.checked)}
                />
              }
              label="Enable IPv6"
            />
            <AppTextField
              disabled={isCreating}
              fullWidth
              label="Subnet"
              name="network-subnet"
              onChange={(event) => setSubnet(event.target.value)}
              placeholder={enableIpv6 ? "2001:db8::/64" : "172.20.0.0/16"}
              value={subnet}
            />
            <AppTextField
              disabled={isCreating}
              error={Boolean(gateway.trim() && !subnet.trim())}
              fullWidth
              helperText={
                gateway.trim() && !subnet.trim()
                  ? "Enter a subnet before setting a gateway."
                  : undefined
              }
              label="Gateway"
              name="network-gateway"
              onChange={(event) => setGateway(event.target.value)}
              placeholder={enableIpv6 ? "2001:db8::1" : "172.20.0.1"}
              value={gateway}
            />
            <section>
              <SectionHeader
                controlsId="network-driver-options"
                expanded={advanced}
                onToggle={() => setAdvanced((current) => !current)}
                title="Advanced"
              />
              <AppCollapse in={advanced} unmountOnExit>
                <div id="network-driver-options">
                  <AppTextField
                    disabled={isCreating}
                    error={Boolean(parsedOptions.error)}
                    fullWidth
                    helperText={
                      parsedOptions.error ??
                      "Optional, one key=value driver option per line."
                    }
                    label="Driver options"
                    multiline
                    name="network-driver-options"
                    onChange={(event) => setOptionsText(event.target.value)}
                    rows={4}
                    value={optionsText}
                  />
                </div>
              </AppCollapse>
            </section>
            {formError && (
              <AppTypography color="error" role="alert" variant="body2">
                {formError}
              </AppTypography>
            )}
          </div>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isCreating} onClick={handleClose} type="button">
            Cancel
          </AppButton>
          <AppButton disabled={isCreating} type="submit" variant="contained">
            {isCreating ? "Creating…" : "Create network"}
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
};
