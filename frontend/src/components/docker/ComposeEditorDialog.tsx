import { useAppTheme } from "@/theme";
import React, {
  Suspense,
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";

import ComposeValidationFeedback, {
  ValidationResult,
} from "./ComposeValidationFeedback";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogTitle,
  AppDialogContent,
  AppDialogActions,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
interface ComposeEditorDialogProps {
  open: boolean;
  mode: "create" | "edit";
  readOnly?: boolean;
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
const FileEditor = React.lazy(
  () => import("@/components/filebrowser/FileEditor"),
);
const ComposeEditorDialog: React.FC<ComposeEditorDialogProps> = ({
  open,
  mode,
  readOnly = false,
  stackName: initialStackName = "",
  filePath = "",
  initialContent = "",
  onClose,
  onSave,
  onValidate,
}) => {
  const theme = useAppTheme();
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
    if (!readOnly && isEditorDirty) {
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
  const handleSave = useCallback(async () => {
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
      let contentToSave = content;

      // Run validation before save
      if (onValidate) {
        const validationResult = await onValidate(content);
        setValidation(validationResult);
        if (!validationResult.valid) {
          setIsSaving(false);
          setIsValidating(false);
          return;
        }

        // Use normalized content if available (auto-adds container_name)
        if (validationResult.normalized_content) {
          contentToSave = validationResult.normalized_content;
        }
      }

      // Save the file (with normalized content)
      await onSave(contentToSave, stackName.trim(), filePath);

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
  }, [mode, stackName, onValidate, onSave, filePath]);

  // Add Ctrl+S keyboard shortcut
  const handleKeyDown = useEffectEvent((e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "s") {
      e.preventDefault();
      if (!isSaving && !isValidating) {
        handleSave();
      }
    }
  });
  useEffect(() => {
    if (!open) return;
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [open]);
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
    setStackName(sanitizeStackName(e.target.value));
  };
  return (
    <>
      <GeneralDialog
        open={open}
        onClose={handleClose}
        maxWidth="lg"
        fullWidth
        fullScreen
        paperStyle={{
          backgroundColor: theme.palette.background.default,
          margin: 0,
          borderRadius: 0,
          border: "none",
          boxShadow: "none",
        }}
      >
        <AppDialogTitle
          style={{
            backgroundColor: theme.header.background,
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 8,
            }}
          >
            <AppTypography variant="h6">
              {readOnly
                ? "View Docker Compose Stack"
                : mode === "create"
                  ? "Create Docker Compose Stack"
                  : "Edit Docker Compose Stack"}
            </AppTypography>

            {mode === "create" ? (
              <AppTextField
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
              <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                <AppTypography variant="body2" color="text.secondary">
                  Stack: <strong>{stackName}</strong>
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  File: {filePath}
                </AppTypography>
              </div>
            )}
          </div>
        </AppDialogTitle>

        <AppDialogContent
          style={{
            padding: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          <ComposeValidationFeedback
            validation={validation}
            isValidating={isValidating}
          />

          <div
            style={{
              flex: 1,
              overflow: "hidden",
            }}
          >
            <Suspense
              fallback={
                <div
                  style={{
                    width: "100%",
                    height: "100%",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  <ComponentLoader />
                </div>
              }
            >
              <FileEditor
                ref={editorRef}
                filePath={filePath || "docker-compose.yml"}
                fileName="docker-compose.yml"
                initialContent={initialContent}
                onSave={handleSave}
                readOnly={readOnly}
                onDirtyChange={readOnly ? undefined : setIsEditorDirty}
              />
            </Suspense>
          </div>
        </AppDialogContent>

        <AppDialogActions
          style={{
            backgroundColor: theme.header.background,
            borderTop: `1px solid ${theme.palette.divider}`,
            padding: 8,
          }}
        >
          {readOnly ? (
            <AppButton onClick={handleClose} variant="contained">
              Close
            </AppButton>
          ) : (
            <>
              <AppButton onClick={handleClose} disabled={isSaving}>
                Cancel
              </AppButton>
              <AppButton
                onClick={handleValidate}
                disabled={isSaving || isValidating}
                variant="outlined"
              >
                {isValidating ? "Validating..." : "Validate"}
              </AppButton>
              <AppButton
                onClick={handleSave}
                disabled={isSaving || isValidating}
                variant="contained"
                color="primary"
              >
                {isSaving ? "Saving..." : "Save"}
              </AppButton>
            </>
          )}
        </AppDialogActions>
      </GeneralDialog>

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
