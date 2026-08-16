import { lazy, Suspense, useRef, useState, type ChangeEvent } from "react";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

import type { ValidationResult } from "./ComposeValidationFeedback";
import ComposeValidationFeedback from "./ComposeValidationFeedback";

interface ComposeEditorDialogProps {
  filePath?: string;
  initialContent?: string;
  mode: "create" | "edit";
  onClose: () => void;
  onSave: (
    content: string,
    stackName: string,
    filePath: string,
  ) => Promise<boolean>;
  onValidate?: (content: string) => Promise<ValidationResult>;
  open: boolean;
  readOnly?: boolean;
  stackName?: string;
}
const FileEditor = lazy(() => import("@/components/filebrowser/FileEditor"));
const ComposeEditorDialog = ({
  open,
  mode,
  readOnly = false,
  stackName: initialStackName = "",
  filePath = "",
  initialContent = "",
  onClose,
  onSave,
  onValidate,
}: ComposeEditorDialogProps) => {
  const theme = useAppTheme();
  const editorRef = useRef<FileEditorHandle>(null);
  const [stackName, setStackName] = useState(initialStackName);
  const [isEditorDirty, setIsEditorDirty] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [showUnsavedDialog, setShowUnsavedDialog] = useState(false);

  const [editorSource, setEditorSource] = useState({
    filePath: "",
    open: false,
  });
  const sourceChanged =
    open !== editorSource.open || (open && filePath !== editorSource.filePath);
  if (sourceChanged) {
    setEditorSource({ filePath, open });
    if (open) {
      setStackName(initialStackName);
      setIsEditorDirty(false);
      setIsSaving(false);
      setValidation(null);
    }
  }
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
    const saved = await editorRef.current?.save();
    if (!saved) return;
    setShowUnsavedDialog(false);
  };
  const handleValidate = async () => {
    const editor = editorRef.current;
    if (!onValidate || !editor) return;
    setIsValidating(true);
    const validate = async () => {
      try {
        const content = editor.getContent();
        const result = await onValidate(content);
        setValidation(result);
      } catch (error) {
        console.error("Validation error:", error);
      }
    };
    await validate().finally(() => setIsValidating(false));
  };
  const handleSave = async (content: string): Promise<boolean> => {
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
      return false;
    }
    setIsSaving(true);
    setIsValidating(true);
    const save = async (): Promise<boolean> => {
      try {
        let contentToSave = content;

        // Run validation before save
        if (onValidate) {
          const validationResult = await onValidate(content);
          setValidation(validationResult);
          if (!validationResult.valid) {
            setIsSaving(false);
            setIsValidating(false);
            return false;
          }

          // Use normalized content if available (auto-adds container_name)
          if (validationResult.normalized_content) {
            contentToSave = validationResult.normalized_content;
          }
        }

        // Save the file (with normalized content)
        return await onSave(contentToSave, stackName.trim(), filePath);
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
        return false;
      }
    };
    return save().finally(() => {
      setIsSaving(false);
      setIsValidating(false);
    });
  };

  const requestSave = () => {
    void editorRef.current?.save();
  };
  const sanitizeStackName = (name: string): string => {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9-_]/g, "-")
      .replace(/^-+|-+$/g, "")
      .substring(0, 63);
  };
  const handleStackNameChange = (
    e: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    setStackName(sanitizeStackName(e.target.value));
  };
  return (
    <>
      <AppFullscreenDialog
        contentStyle={{
          backgroundColor: theme.palette.background.default,
        }}
        onClose={handleClose}
        open={open}
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
                disabled={isSaving}
                fullWidth
                helperText="Lowercase letters, numbers, hyphens, and underscores only (max 63 chars)"
                label="Stack Name"
                onChange={handleStackNameChange}
                placeholder="my-stack"
                size="small"
                value={stackName}
              />
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                <AppTypography color="text.secondary" variant="body2">
                  Stack: <strong>{stackName}</strong>
                </AppTypography>
                <AppTypography color="text.secondary" variant="caption">
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
            isValidating={isValidating}
            validation={validation}
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
                fileName="docker-compose.yml"
                filePath={filePath || "docker-compose.yml"}
                initialContent={initialContent}
                isSaving={isSaving || isValidating}
                onDirtyChange={readOnly ? undefined : setIsEditorDirty}
                onSave={handleSave}
                readOnly={readOnly}
                ref={editorRef}
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
              <AppButton disabled={isSaving} onClick={handleClose}>
                Cancel
              </AppButton>
              <AppButton
                disabled={isSaving || isValidating}
                onClick={handleValidate}
                variant="outlined"
              >
                {isValidating ? "Validating..." : "Validate"}
              </AppButton>
              <AppButton
                color="primary"
                disabled={isSaving || isValidating}
                onClick={requestSave}
                variant="contained"
              >
                {isSaving ? "Saving..." : "Save"}
              </AppButton>
            </>
          )}
        </AppDialogActions>
      </AppFullscreenDialog>

      <UnsavedChangesDialog
        isSaving={isSaving}
        onDiscardAndExit={handleDiscardAndExit}
        onKeepEditing={() => setShowUnsavedDialog(false)}
        onSaveAndExit={handleSaveAndExit}
        open={showUnsavedDialog}
      />
    </>
  );
};
export default ComposeEditorDialog;
