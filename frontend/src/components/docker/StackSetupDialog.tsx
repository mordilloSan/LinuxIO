import { useAppTheme } from "@/theme";
import React, { useEffect, useState } from "react";
import { toast } from "sonner";

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
import { alpha } from "@/utils/color";
interface StackSetupDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (stackName: string, workingDir: string) => void;
  defaultWorkingDir?: string;
}
const StackSetupDialog: React.FC<StackSetupDialogProps> = ({
  open,
  onClose,
  onConfirm,
  defaultWorkingDir,
}) => {
  const theme = useAppTheme();
  const [stackName, setStackName] = useState("");
  const [workingDir, setWorkingDir] = useState("");
  const [isWorkingDirManuallyEdited, setIsWorkingDirManuallyEdited] =
    useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [errors, setErrors] = useState<{
    stackName?: string;
    workingDir?: string;
  }>({});

  // Reset state when dialog opens
  useEffect(() => {
    if (open) {
      setStackName("");
      setWorkingDir(defaultWorkingDir || "");
      setIsWorkingDirManuallyEdited(false);
      setErrors({});
    }
  }, [open, defaultWorkingDir]);
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
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
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
            label="Stack Name"
            value={stackName}
            onChange={handleStackNameChange}
            fullWidth
            placeholder="my-stack"
            helperText={
              errors.stackName ||
              "Lowercase letters, numbers, hyphens, and underscores only (max 63 chars)"
            }
            error={!!errors.stackName}
            autoFocus
          />

          <AppTextField
            label="Working Directory"
            value={workingDir}
            onChange={handleWorkingDirChange}
            fullWidth
            placeholder="/path/to/stack"
            helperText={
              errors.workingDir ||
              "Absolute path where the docker-compose.yml file will be saved"
            }
            error={!!errors.workingDir}
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
            <AppTypography variant="caption" color="text.secondary">
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
        <AppButton onClick={onClose} disabled={isValidating}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleConfirm}
          variant="contained"
          color="primary"
          disabled={isValidating}
          startIcon={
            isValidating ? <AppCircularProgress size={20} /> : undefined
          }
        >
          {isValidating ? "Validating..." : "Next"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default StackSetupDialog;
