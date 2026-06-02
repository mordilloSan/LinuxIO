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
  configFiles: string[];
  isLoading?: boolean;
  onClose: () => void;
  onConfirm: (option: DeleteOption) => void;
  open: boolean;
  projectName: string;
  workingDir: string;
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
      fullWidth
      maxWidth="sm"
      onClose={handleClose}
      open={open}
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
          color={theme.palette.error.main}
          height={24}
          icon="mdi:delete"
          width={24}
        />
        <AppTypography variant="h6">Delete Stack: {projectName}</AppTypography>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingTop: 12,
        }}
      >
        <AppTypography color="text.secondary" gutterBottom variant="body2">
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
                aria-pressed={isSelected}
                disabled={isLoading}
                key={option.value}
                onClick={() => setDeleteOption(option.value)}
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
                type="button"
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
                    color={option.color === "error" ? "error" : undefined}
                    variant="body1"
                  >
                    {option.title}
                  </AppTypography>
                  <AppTypography color="text.secondary" variant="caption">
                    {option.description}
                  </AppTypography>
                </div>
                {isSelected && (
                  <Icon
                    color={accentColor}
                    height={20}
                    icon={
                      option.color === "error"
                        ? "mdi:alert-circle"
                        : "mdi:check-circle"
                    }
                    width={20}
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
        <AppButton color="inherit" disabled={isLoading} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color={deleteOption === "directory" ? "error" : "primary"}
          disabled={isLoading}
          onClick={handleConfirm}
          startIcon={
            deleteOption === "directory" ? (
              <Icon height={20} icon="mdi:folder-remove" width={20} />
            ) : (
              <Icon height={20} icon="mdi:delete" width={20} />
            )
          }
          variant="contained"
        >
          {isLoading ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default DeleteStackDialog;
