import { Icon } from "@iconify/react";
import React, { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
export type DeleteOption = "containers" | "file" | "directory";
interface DeleteStackDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (option: DeleteOption) => void;
  projectName: string;
  configFiles: string[];
  workingDir: string;
  isLoading?: boolean;
}
const DeleteStackDialog: React.FC<DeleteStackDialogProps> = ({
  open,
  onClose,
  onConfirm,
  projectName,
  configFiles,
  workingDir,
  isLoading = false,
}) => {
  const theme = useAppTheme();
  const [deleteOption, setDeleteOption] = useState<DeleteOption>("containers");

  const deleteOptions: Array<{
    value: DeleteOption;
    title: string;
    description: string;
    color?: "error";
  }> = [
    {
      value: "containers",
      title: "Remove containers only",
      description:
        "Runs docker compose down, removes containers and networks, keeps compose file",
    },
    {
      value: "file",
      title: "Remove containers + delete compose file",
      description:
        configFiles.length > 0
          ? `Will delete: ${configFiles[0]}`
          : "Deletes the compose file and keeps the rest of the directory",
    },
    {
      value: "directory",
      title: "Remove containers + delete entire directory",
      description:
        workingDir.length > 0
          ? `Will delete: ${workingDir}`
          : "Deletes the entire stack directory and its contents",
      color: "error",
    },
  ];

  const handleConfirm = () => {
    onConfirm(deleteOption);
  };
  const handleClose = () => {
    if (!isLoading) {
      setDeleteOption("containers");
      onClose();
    }
  };
  return (
    <GeneralDialog
      open={open}
      onClose={handleClose}
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
          display: "flex",
          alignItems: "center",
          gap: 4,
        }}
      >
        <Icon
          icon="mdi:delete"
          width={24}
          height={24}
          color={theme.palette.error.main}
        />
        <AppTypography variant="h6">Delete Stack: {projectName}</AppTypography>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingTop: 12,
        }}
      >
        <AppTypography variant="body2" color="text.secondary" gutterBottom>
          Choose what to delete:
        </AppTypography>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 8,
          }}
        >
          {deleteOptions.map((option) => {
            const isSelected = deleteOption === option.value;
            const accentColor =
              option.color === "error"
                ? theme.palette.error.main
                : theme.palette.primary.main;

            return (
              <button
                key={option.value}
                type="button"
                onClick={() => setDeleteOption(option.value)}
                disabled={isLoading}
                aria-pressed={isSelected}
                style={{
                  width: "100%",
                  display: "flex",
                  alignItems: "flex-start",
                  justifyContent: "space-between",
                  gap: 12,
                  padding: "12px 14px",
                  borderRadius: 10,
                  border: `1px solid ${
                    isSelected ? accentColor : theme.palette.divider
                  }`,
                  backgroundColor: isSelected
                    ? theme.palette.action.selected
                    : theme.palette.background.paper,
                  cursor: isLoading ? "default" : "pointer",
                  textAlign: "left",
                }}
              >
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 2,
                    flex: 1,
                  }}
                >
                  <AppTypography
                    variant="body1"
                    color={option.color === "error" ? "error" : undefined}
                  >
                    {option.title}
                  </AppTypography>
                  <AppTypography variant="caption" color="text.secondary">
                    {option.description}
                  </AppTypography>
                </div>
                {isSelected && (
                  <Icon
                    icon={
                      option.color === "error"
                        ? "mdi:alert-circle"
                        : "mdi:check-circle"
                    }
                    width={20}
                    height={20}
                    color={accentColor}
                  />
                )}
              </button>
            );
          })}
        </div>

        {deleteOption === "directory" && (
          <AppAlert
            severity="warning"
            style={{
              marginTop: 8,
            }}
          >
            <AppTypography variant="body2">
              <strong>Warning:</strong> This will permanently delete the entire
              stack directory including all configuration files, data, and
              subdirectories. This action cannot be undone!
            </AppTypography>
          </AppAlert>
        )}

        {deleteOption === "file" && (
          <AppAlert
            severity="info"
            style={{
              marginTop: 8,
            }}
          >
            <AppTypography variant="body2">
              The compose file will be deleted, but volumes and other data in
              the directory will be preserved.
            </AppTypography>
          </AppAlert>
        )}
      </AppDialogContent>

      <AppDialogActions
        style={{
          padding: 8,
          borderTop: `1px solid ${theme.palette.divider}`,
        }}
      >
        <AppButton onClick={handleClose} disabled={isLoading} color="inherit">
          Cancel
        </AppButton>
        <AppButton
          onClick={handleConfirm}
          disabled={isLoading}
          variant="contained"
          color={deleteOption === "directory" ? "error" : "primary"}
          startIcon={
            deleteOption === "directory" ? (
              <Icon icon="mdi:folder-remove" width={20} height={20} />
            ) : (
              <Icon icon="mdi:delete" width={20} height={20} />
            )
          }
        >
          {isLoading ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default DeleteStackDialog;
