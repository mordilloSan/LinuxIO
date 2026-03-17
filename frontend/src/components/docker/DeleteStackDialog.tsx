import { Icon } from "@iconify/react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  FormControlLabel,
  Radio,
  RadioGroup,
  Alert,
  useTheme,
} from "@mui/material";
import React, { useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
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
  const theme = useTheme();
  const [deleteOption, setDeleteOption] = useState<DeleteOption>("containers");
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
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.palette.background.default,
          },
        },
      }}
    >
      <DialogTitle
        sx={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          display: "flex",
          alignItems: "center",
          gap: 1,
        }}
      >
        <Icon
          icon="mdi:delete"
          width={24}
          height={24}
          color={theme.palette.error.main}
        />
        <AppTypography variant="h6">Delete Stack: {projectName}</AppTypography>
      </DialogTitle>

      <DialogContent
        sx={{
          pt: 3,
        }}
      >
        <AppTypography variant="body2" color="text.secondary" gutterBottom>
          Choose what to delete:
        </AppTypography>

        <RadioGroup
          value={deleteOption}
          onChange={(e) => setDeleteOption(e.target.value as DeleteOption)}
        >
          <FormControlLabel
            value="containers"
            control={<Radio />}
            label={
              <div>
                <AppTypography variant="body1">
                  Remove containers only
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  Runs `docker compose down` - removes containers and networks,
                  keeps compose file
                </AppTypography>
              </div>
            }
            sx={{
              alignItems: "flex-start",
              mb: 1,
            }}
          />

          <FormControlLabel
            value="file"
            control={<Radio />}
            label={
              <div>
                <AppTypography variant="body1">
                  Remove containers + delete compose file
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  {configFiles.length > 0 && `Will delete: ${configFiles[0]}`}
                </AppTypography>
              </div>
            }
            sx={{
              alignItems: "flex-start",
              mb: 1,
            }}
          />

          <FormControlLabel
            value="directory"
            control={<Radio color="error" />}
            label={
              <div>
                <AppTypography variant="body1" color="error">
                  Remove containers + delete entire directory
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  {workingDir && `Will delete: ${workingDir}`}
                </AppTypography>
              </div>
            }
            sx={{
              alignItems: "flex-start",
            }}
          />
        </RadioGroup>

        {deleteOption === "directory" && (
          <Alert
            severity="warning"
            icon={<Icon icon="mdi:alert" width={22} height={22} />}
            sx={{
              mt: 2,
            }}
          >
            <AppTypography variant="body2">
              <strong>Warning:</strong> This will permanently delete the entire
              stack directory including all configuration files, data, and
              subdirectories. This action cannot be undone!
            </AppTypography>
          </Alert>
        )}

        {deleteOption === "file" && (
          <Alert
            severity="info"
            sx={{
              mt: 2,
            }}
          >
            <AppTypography variant="body2">
              The compose file will be deleted, but volumes and other data in
              the directory will be preserved.
            </AppTypography>
          </Alert>
        )}
      </DialogContent>

      <DialogActions
        sx={{
          p: 2,
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
      </DialogActions>
    </Dialog>
  );
};
export default DeleteStackDialog;
