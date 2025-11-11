import { Box, Paper, Typography } from "@mui/material";
import React, {
  useState,
  useEffect,
  useImperativeHandle,
  forwardRef,
} from "react";

interface FileEditorProps {
  filePath: string;
  fileName: string;
  initialContent: string;
  onSave: (content: string) => Promise<void>;
  isSaving?: boolean;
}

export interface FileEditorHandle {
  save: () => Promise<void>;
  getContent: () => string;
}

const FileEditor = forwardRef<FileEditorHandle, FileEditorProps>(
  ({ filePath, fileName, initialContent, onSave, isSaving = false }, ref) => {
    const [content, setContent] = useState(initialContent);
    const [isDirty, setIsDirty] = useState(false);

    useEffect(() => {
      setContent(initialContent);
      setIsDirty(false);
    }, [filePath, initialContent]);

    const handleContentChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      setContent(e.target.value);
      setIsDirty(e.target.value !== initialContent);
    };

    const handleSave = async () => {
      try {
        await onSave(content);
        setIsDirty(false);
      } catch {
        // Error is handled by parent component
      }
    };

    useImperativeHandle(ref, () => ({
      save: handleSave,
      getContent: () => content,
    }));

    return (
      <Paper
        variant="outlined"
        sx={{
          borderRadius: 2,
          display: "flex",
          flexDirection: "column",
          p: 2,
          gap: 2,
          height: "100%",
        }}
      >
        {/* Header with file name */}
        <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          <Box sx={{ flex: 1 }}>
            <Typography variant="h6" fontWeight={600}>
              {fileName}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {filePath}
            </Typography>
            {isDirty && (
              <Typography
                variant="caption"
                color="warning.main"
                sx={{ mt: 0.5 }}
              >
                • Unsaved changes
              </Typography>
            )}
          </Box>
        </Box>

        {/* Editor area */}
        <Box
          component="textarea"
          value={content}
          onChange={handleContentChange}
          disabled={isSaving}
          sx={{
            flex: 1,
            fontFamily: "monospace",
            fontSize: "0.875rem",
            padding: 2,
            borderRadius: 1,
            border: "1px solid",
            borderColor: "divider",
            backgroundColor: (theme) =>
              theme.palette.mode === "light" ? "#f5f5f5" : "#1e1e1e",
            color: "text.primary",
            resize: "none",
            "&:focus": {
              outline: "none",
              borderColor: "primary.main",
              boxShadow: (theme) => `0 0 0 2px ${theme.palette.primary.main}22`,
            },
            "&:disabled": {
              opacity: 0.6,
              cursor: "not-allowed",
            },
          }}
        />

        {/* Status message */}
        {isSaving && (
          <Typography variant="body2" color="info.main">
            Saving...
          </Typography>
        )}
      </Paper>
    );
  },
);

FileEditor.displayName = "FileEditor";

export default FileEditor;
