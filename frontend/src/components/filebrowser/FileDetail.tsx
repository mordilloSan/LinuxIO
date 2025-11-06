import React from "react";
import { Box, Button, Paper, Typography, alpha } from "@mui/material";
import { Download as DownloadIcon } from "@mui/icons-material";
import { FileResource } from "./types";

interface FileDetailProps {
  resource: FileResource;
  onDownload: (path: string) => void;
}

const FileDetail: React.FC<FileDetailProps> = ({ resource, onDownload }) => {
  return (
    <Paper
      variant="outlined"
      sx={{
        borderRadius: 2,
        display: "flex",
        flexDirection: "column",
        p: 3,
        gap: 2,
      }}
    >
      <Typography variant="h6" fontWeight={600}>
        {resource.name}
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {resource.type || "File"}
      </Typography>

      {resource.content && (
        <Box
          sx={{
            maxHeight: 320,
            overflowY: "auto",
            p: 2,
            borderRadius: 1,
            backgroundColor: (theme) =>
              alpha(theme.palette.text.primary, 0.04),
            fontFamily: "monospace",
            fontSize: "0.875rem",
          }}
        >
          <pre
            style={{
              margin: 0,
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {resource.content}
          </pre>
        </Box>
      )}

      <Button
        variant="contained"
        startIcon={<DownloadIcon />}
        onClick={() => onDownload(resource.path)}
        sx={{ alignSelf: "flex-start" }}
      >
        Download
      </Button>
    </Paper>
  );
};

export default FileDetail;
