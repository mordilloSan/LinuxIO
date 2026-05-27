import React, { useState } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
interface StackSetupDialogProps {
  defaultWorkingDir?: string;
  onClose: () => void;
  onConfirm: (stackName: string, workingDir: string) => void;
  open: boolean;
}
const StackSetupDialog: React.FC<StackSetupDialogProps> = ({
  open,
  onClose,
  onConfirm,
  defaultWorkingDir,
}) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const [stackName, setStackName] = useState("");
  const [workingDir, setWorkingDir] = useState("");
  const [isWorkingDirManuallyEdited, setIsWorkingDirManuallyEdited] =
    useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [errors, setErrors] = useState<{
    stackName?: string;
    workingDir?: string;
  }>({});

  const [prevOpen, setPrevOpen] = useState(false);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setStackName("");
      setWorkingDir(defaultWorkingDir || "");
      setIsWorkingDirManuallyEdited(false);
      setErrors({});
    }
  }
  const sanitizeStackName = (name: string): string => {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9-_]/g, "-")
      .replace(/^-+|-+$/g, "")
      .substring(0, 63);
  };
  const handleStackNameChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    const sanitized = sanitizeStackName(e.target.value);
    setStackName(sanitized);

    // Auto-update working directory if not manually edited
    if (!isWorkingDirManuallyEdited && defaultWorkingDir) {
      const newWorkingDir = sanitized
        ? `${defaultWorkingDir}/${sanitized}`
        : defaultWorkingDir;
      setWorkingDir(newWorkingDir);
    }
    if (errors.stackName) {
      setErrors({
        ...errors,
        stackName: undefined,
      });
    }
  };
  const handleWorkingDirChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    setWorkingDir(e.target.value);
    setIsWorkingDirManuallyEdited(true);
    if (errors.workingDir) {
      setErrors({
        ...errors,
        workingDir: undefined,
      });
    }
  };
  const validate = (): boolean => {
    const newErrors: {
      stackName?: string;
      workingDir?: string;
    } = {};
    if (!stackName.trim()) {
      newErrors.stackName = "Stack name is required";
    }
    if (!workingDir.trim()) {
      newErrors.workingDir = "Working directory is required";
    } else if (!workingDir.startsWith("/")) {
      newErrors.workingDir = "Working directory must be an absolute path";
    }
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };
  const handleConfirm = async () => {
    if (!validate()) {
      return;
    }
    setIsValidating(true);
    try {
      // Validate the directory with the backend
      const result = await linuxio.docker.validate_stack_directory.call(
        workingDir.trim(),
      );
      if (!result.valid) {
        setErrors({
          ...errors,
          workingDir: result.error || "Invalid directory",
        });
        toast.error(result.error || "Invalid directory");
        setIsValidating(false);
        return;
      }

      // Directory is valid, proceed
      onConfirm(stackName.trim(), workingDir.trim());
    } catch (error) {
      toast.error(
        `Failed to validate directory: ${error instanceof Error ? error.message : "Unknown error"}`,
      );
    } finally {
      setIsValidating(false);
    }
  };
  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
      paperStyle={{
        backgroundColor: theme.palette.background.default,
      }}
    >
      <AppDialogTitle
        style={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
        }}
      >
        <AppTypography variant="h6">
          Create New Docker Compose Stack
        </AppTypography>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingTop: 12,
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(3),
          }}
        >
          <AppTextField
            autoFocus
            error={!!errors.stackName}
            fullWidth
            helperText={
              errors.stackName ||
              "Lowercase letters, numbers, hyphens, and underscores only (max 63 chars)"
            }
            label="Stack Name"
            onChange={handleStackNameChange}
            placeholder="my-stack"
            value={stackName}
          />

          <AppTextField
            error={!!errors.workingDir}
            fullWidth
            helperText={
              errors.workingDir ||
              "Absolute path where the docker-compose.yml file will be saved"
            }
            label="Working Directory"
            onChange={handleWorkingDirChange}
            placeholder="/path/to/stack"
            value={workingDir}
          />

          <div
            style={{
              backgroundColor: alpha(
                theme.palette.text.primary,
                theme.palette.mode === "dark" ? 0.05 : 0.02,
              ),
              borderRadius: theme.shape.borderRadius,
              padding: theme.spacing(2),
            }}
          >
            <AppTypography color="text.secondary" variant="caption">
              <strong>File location:</strong>
              <br />
              {workingDir && stackName
                ? `${workingDir}/docker-compose.yml`
                : "Enter stack name and directory"}
            </AppTypography>
          </div>
        </div>
      </AppDialogContent>

      <AppDialogActions
        style={{
          backgroundColor: theme.header.background,
          borderTop: `1px solid ${theme.palette.divider}`,
          padding: 8,
        }}
      >
        <AppButton disabled={isValidating} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="primary"
          disabled={isValidating}
          onClick={handleConfirm}
          startIcon={
            isValidating ? <AppCircularProgress size={20} /> : undefined
          }
          variant="contained"
        >
          {isValidating ? "Validating..." : "Next"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default StackSetupDialog;
