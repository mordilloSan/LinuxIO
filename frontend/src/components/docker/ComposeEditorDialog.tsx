import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
  Typography,
  useTheme,
} from "@mui/material";
import React, { useEffect, useRef, useState } from "react";

import ComposeValidationFeedback, {
  ValidationResult,
} from "./ComposeValidationFeedback";

import FileEditor, {
  FileEditorHandle,
} from "@/components/filebrowser/FileEditor";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";

interface ComposeEditorDialogProps {
  open: boolean;
  mode: "create" | "edit";
  stackName?: string;
  filePath?: string;
  initialContent?: string;
  onClose: () => void;
  onSave: (
    content: string,
    stackName: string,
    filePath: string,
  ) => Promise<void>;
  onValidate?: (content: string) => Promise<ValidationResult>;
}

const ComposeEditorDialog: React.FC<ComposeEditorDialogProps> = ({
  open,
  mode,
  stackName: initialStackName = "",
  filePath = "",
  initialContent = "",
  onClose,
  onSave,
  onValidate,
}) => {
  const theme = useTheme();
  const editorRef = useRef<FileEditorHandle>(null);

  const [stackName, setStackName] = useState(initialStackName);
  const [isEditorDirty, setIsEditorDirty] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [showUnsavedDialog, setShowUnsavedDialog] = useState(false);

  // Reset state when dialog opens/closes
  useEffect(() => {
    if (open) {
      setStackName(initialStackName);
      setIsEditorDirty(false);
      setIsSaving(false);
      setValidation(null);
    }
  }, [open, initialStackName]);

  const handleClose = () => {
    if (isEditorDirty) {
      setShowUnsavedDialog(true);
    } else {
      onClose();
    }
  };

  const handleDiscardAndExit = () => {
    setShowUnsavedDialog(false);
    onClose();
  };

  const handleSaveAndExit = async () => {
    await handleSave();
    setShowUnsavedDialog(false);
  };

  const handleValidate = async () => {
    if (!onValidate || !editorRef.current) return;

    setIsValidating(true);
    try {
      const content = editorRef.current.getContent();
      const result = await onValidate(content);
      setValidation(result);
    } catch (error) {
      console.error("Validation error:", error);
    } finally {
      setIsValidating(false);
    }
  };

  const handleSave = async () => {
    if (!editorRef.current) return;

    // Validate stack name for create mode
    if (mode === "create" && !stackName.trim()) {
      setValidation({
        valid: false,
        errors: [
          {
            message: "Stack name is required",
            type: "error",
          },
        ],
      });
      return;
    }

    setIsSaving(true);
    setIsValidating(true);

    try {
      const content = editorRef.current.getContent();

      // Run validation before save
      if (onValidate) {
        const validationResult = await onValidate(content);
        setValidation(validationResult);

        if (!validationResult.valid) {
          setIsSaving(false);
          setIsValidating(false);
          return;
        }
      }

      // Save the file
      await onSave(content, stackName.trim(), filePath);

      // Reset dirty state after successful save
      setIsEditorDirty(false);
    } catch (error) {
      console.error("Save error:", error);
      setValidation({
        valid: false,
        errors: [
          {
            message:
              error instanceof Error ? error.message : "Failed to save file",
            type: "error",
          },
        ],
      });
    } finally {
      setIsSaving(false);
      setIsValidating(false);
    }
  };

  const sanitizeStackName = (name: string): string => {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9-_]/g, "-")
      .replace(/^-+|-+$/g, "")
      .substring(0, 63);
  };

  const handleStackNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setStackName(sanitizeStackName(e.target.value));
  };

  return (
    <>
      <Dialog
        open={open}
        onClose={handleClose}
        maxWidth="lg"
        fullWidth
        fullScreen
        slotProps={{
          paper: {
            sx: {
              backgroundColor: theme.palette.background.default,
              m: 0,
            },
          },
        }}
      >
        <DialogTitle
          sx={{
            backgroundColor: theme.header.background,
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
            <Typography variant="h6">
              {mode === "create"
                ? "Create Docker Compose Stack"
                : "Edit Docker Compose Stack"}
            </Typography>

            {mode === "create" ? (
              <TextField
                label="Stack Name"
                value={stackName}
                onChange={handleStackNameChange}
                fullWidth
                size="small"
                placeholder="my-stack"
                helperText="Lowercase letters, numbers, hyphens, and underscores only (max 63 chars)"
                disabled={isSaving}
              />
            ) : (
              <Box>
                <Typography variant="body2" color="text.secondary">
                  Stack: <strong>{stackName}</strong>
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  File: {filePath}
                </Typography>
              </Box>
            )}
          </Box>
        </DialogTitle>

        <DialogContent sx={{ p: 0, display: "flex", flexDirection: "column" }}>
          <ComposeValidationFeedback
            validation={validation}
            isValidating={isValidating}
          />

          <Box sx={{ flex: 1, overflow: "hidden" }}>
            <FileEditor
              ref={editorRef}
              filePath={filePath || "docker-compose.yml"}
              fileName="docker-compose.yml"
              initialContent={initialContent}
              onSave={handleSave}
              onDirtyChange={setIsEditorDirty}
            />
          </Box>
        </DialogContent>

        <DialogActions
          sx={{
            backgroundColor: theme.header.background,
            borderTop: `1px solid ${theme.palette.divider}`,
            p: 2,
          }}
        >
          <Button onClick={handleClose} disabled={isSaving}>
            Cancel
          </Button>
          <Button
            onClick={handleValidate}
            disabled={isSaving || isValidating}
            variant="outlined"
          >
            {isValidating ? "Validating..." : "Validate"}
          </Button>
          <Button
            onClick={handleSave}
            disabled={isSaving || isValidating}
            variant="contained"
            color="primary"
          >
            {isSaving ? "Saving..." : "Save"}
          </Button>
        </DialogActions>
      </Dialog>

      <UnsavedChangesDialog
        open={showUnsavedDialog}
        onKeepEditing={() => setShowUnsavedDialog(false)}
        onDiscardAndExit={handleDiscardAndExit}
        onSaveAndExit={handleSaveAndExit}
        isSaving={isSaving}
      />
    </>
  );
};

export default ComposeEditorDialog;
