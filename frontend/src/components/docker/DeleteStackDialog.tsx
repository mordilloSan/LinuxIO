import DeleteIcon from "@mui/icons-material/Delete";
import FolderDeleteIcon from "@mui/icons-material/FolderDelete";
import WarningIcon from "@mui/icons-material/Warning";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Typography,
  FormControlLabel,
  Radio,
  RadioGroup,
  Box,
  Alert,
  useTheme,
} from "@mui/material";
import React, { useState } from "react";

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
        <DeleteIcon color="error" />
        <Typography variant="h6">Delete Stack: {projectName}</Typography>
      </DialogTitle>

      <DialogContent sx={{ pt: 3 }}>
        <Typography variant="body2" color="text.secondary" gutterBottom>
          Choose what to delete:
        </Typography>

        <RadioGroup
          value={deleteOption}
          onChange={(e) => setDeleteOption(e.target.value as DeleteOption)}
        >
          <FormControlLabel
            value="containers"
            control={<Radio />}
            label={
              <Box>
                <Typography variant="body1">Remove containers only</Typography>
                <Typography variant="caption" color="text.secondary">
                  Runs `docker compose down` - removes containers and networks,
                  keeps compose file
                </Typography>
              </Box>
            }
            sx={{ alignItems: "flex-start", mb: 1 }}
          />

          <FormControlLabel
            value="file"
            control={<Radio />}
            label={
              <Box>
                <Typography variant="body1">
                  Remove containers + delete compose file
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {configFiles.length > 0 && `Will delete: ${configFiles[0]}`}
                </Typography>
              </Box>
            }
            sx={{ alignItems: "flex-start", mb: 1 }}
          />

          <FormControlLabel
            value="directory"
            control={<Radio color="error" />}
            label={
              <Box>
                <Typography variant="body1" color="error">
                  Remove containers + delete entire directory
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {workingDir && `Will delete: ${workingDir}`}
                </Typography>
              </Box>
            }
            sx={{ alignItems: "flex-start" }}
          />
        </RadioGroup>

        {deleteOption === "directory" && (
          <Alert severity="warning" icon={<WarningIcon />} sx={{ mt: 2 }}>
            <Typography variant="body2">
              <strong>Warning:</strong> This will permanently delete the entire
              stack directory including all configuration files, data, and
              subdirectories. This action cannot be undone!
            </Typography>
          </Alert>
        )}

        {deleteOption === "file" && (
          <Alert severity="info" sx={{ mt: 2 }}>
            <Typography variant="body2">
              The compose file will be deleted, but volumes and other data in
              the directory will be preserved.
            </Typography>
          </Alert>
        )}
      </DialogContent>

      <DialogActions
        sx={{ p: 2, borderTop: `1px solid ${theme.palette.divider}` }}
      >
        <Button onClick={handleClose} disabled={isLoading} color="inherit">
          Cancel
        </Button>
        <Button
          onClick={handleConfirm}
          disabled={isLoading}
          variant="contained"
          color={deleteOption === "directory" ? "error" : "primary"}
          startIcon={
            deleteOption === "directory" ? <FolderDeleteIcon /> : <DeleteIcon />
          }
        >
          {isLoading ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default DeleteStackDialog;
