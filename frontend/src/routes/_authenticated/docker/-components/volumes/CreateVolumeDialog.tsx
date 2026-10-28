import { useState, type SyntheticEvent } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

import { parseDockerKeyValueLines } from "../shared/dockerKeyValues";
import { DOCKER_RESOURCE_NAME } from "../shared/dockerResourceName";
import { DOCKER_TOAST_META } from "../shared/dockerToast";

const CUSTOM_VOLUME_DRIVER = "__custom__";

export const CreateVolumeDialog = ({
  drivers,
  existingNames,
  onClose,
  open,
}: {
  drivers: string[];
  existingNames: string[];
  onClose: () => void;
  open: boolean;
}) => {
  const [name, setName] = useState("");
  const [driver, setDriver] = useState("local");
  const [customDriver, setCustomDriver] = useState("");
  const [labelsText, setLabelsText] = useState("");
  const [formError, setFormError] = useState<string>();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const parsedLabels = parseDockerKeyValueLines(labelsText);
  const trimmedName = name.trim();
  const selectedDriver =
    driver === CUSTOM_VOLUME_DRIVER ? customDriver.trim() : driver;
  const nameTaken = existingNames.includes(trimmedName);
  const nameInvalid =
    trimmedName !== "" && !DOCKER_RESOURCE_NAME.test(trimmedName);
  const { mutate: createVolume, isPending } =
    linuxio.docker.create_volume.useMutation({
      success: () => {
        toast.success(`Volume "${trimmedName}" created`);
        closeDialog();
      },
      error: "Failed to create volume",
      toast: DOCKER_TOAST_META,
    });

  const closeDialog = () => {
    setName("");
    setDriver("local");
    setCustomDriver("");
    setLabelsText("");
    setFormError(undefined);
    onClose();
  };
  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!trimmedName || !selectedDriver) {
      setFormError("Name and driver are required.");
      return;
    }
    if (nameTaken) {
      setFormError("Choose a volume name that is not already in use.");
      return;
    }
    if (nameInvalid) {
      setFormError(
        "Name must start with an alphanumeric character and use only letters, numbers, underscores, periods, or hyphens.",
      );
      return;
    }
    if (parsedLabels.error) {
      setFormError(parsedLabels.error);
      return;
    }
    setFormError(undefined);
    createVolume({
      name: trimmedName,
      driver: selectedDriver,
      labels:
        Object.keys(parsedLabels.values).length > 0
          ? parsedLabels.values
          : undefined,
    });
  };

  return (
    <GeneralDialog
      aria-label="Create volume"
      fullWidth
      maxWidth="xs"
      onClose={isPending ? undefined : closeDialog}
      open={open}
    >
      <form className="app-dialog-form" onSubmit={handleSubmit}>
        <AppDialogTitle>Create volume</AppDialogTitle>
        <AppDialogContent>
          <div className="app-dialog-fields">
            <AppTextField
              autoFocus
              disabled={isPending}
              error={nameTaken || nameInvalid}
              fullWidth
              helperText={
                nameTaken
                  ? "This volume name already exists."
                  : nameInvalid
                    ? "Use letters, numbers, underscores, periods, or hyphens."
                    : undefined
              }
              label="Name"
              name="volume-name"
              onChange={(event) => setName(event.target.value)}
              required
              value={name}
            />
            <AppSelect
              disabled={isPending}
              fullWidth
              label="Driver"
              onChange={(event) => setDriver(event.target.value)}
              value={driver}
            >
              {drivers.map((volumeDriver) => (
                <option key={volumeDriver} value={volumeDriver}>
                  {volumeDriver}
                </option>
              ))}
              <option value={CUSTOM_VOLUME_DRIVER}>Custom…</option>
            </AppSelect>
            {driver === CUSTOM_VOLUME_DRIVER && (
              <AppTextField
                autoFocus
                disabled={isPending}
                fullWidth
                label="Custom driver"
                name="volume-custom-driver"
                onChange={(event) => setCustomDriver(event.target.value)}
                required
                value={customDriver}
              />
            )}
            <AppTextField
              disabled={isPending}
              error={Boolean(parsedLabels.error)}
              fullWidth
              helperText={
                parsedLabels.error ?? "Optional, one key=value label per line."
              }
              label="Labels"
              multiline
              name="volume-labels"
              onChange={(event) => setLabelsText(event.target.value)}
              rows={4}
              value={labelsText}
            />
            {formError && (
              <AppTypography color="error" role="alert" variant="body2">
                {formError}
              </AppTypography>
            )}
          </div>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isPending} onClick={closeDialog} type="button">
            Cancel
          </AppButton>
          <AppButton disabled={isPending} type="submit" variant="contained">
            {isPending ? "Creating…" : "Create volume"}
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
};
