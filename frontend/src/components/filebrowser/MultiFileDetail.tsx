import DownloadIcon from "@mui/icons-material/Download";
import { Box, Button, Divider, Paper, Stack, Typography } from "@mui/material";
import React from "react";

import { MultiStatsItem } from "../../types/filebrowser";

import { formatFileSize } from "@/utils/formaters";

interface MultiFileDetailProps {
  multiItems: MultiStatsItem[];
  onDownload: (path: string) => void;
  totalSize?: number | null;
  totalFiles?: number | null;
  totalFolders?: number | null;
}

const DetailRow: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => (
  <Box sx={{ display: "flex", gap: 2 }}>
    <Typography
      variant="body2"
      fontWeight={600}
      color="text.secondary"
      sx={{ minWidth: 140 }}
    >
      {label}:
    </Typography>
    <Typography variant="body2" sx={{ flex: 1, wordBreak: "break-all" }}>
      {value}
    </Typography>
  </Box>
);

const MultiFileDetail: React.FC<MultiFileDetailProps> = ({
  multiItems,
  onDownload,
  totalSize,
  totalFiles,
  totalFolders,
}) => {
  if (!multiItems?.length) {
    return null;
  }

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
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: 2,
        }}
      >
        <Box>
          <Typography variant="h6" fontWeight={600}>
            {multiItems.length} Selected Item
            {multiItems.length === 1 ? "" : "s"}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Combined statistics for the selected files and folders
          </Typography>
        </Box>
      </Box>

      <Divider />

      <Stack spacing={1.5}>
        <DetailRow
          label="Selected Items"
          value={multiItems.length.toLocaleString()}
        />
        <DetailRow label="Aggregate Size" value={formatFileSize(totalSize)} />
        <DetailRow
          label="Files (including nested)"
          value={
            totalFiles !== undefined && totalFiles !== null
              ? totalFiles.toLocaleString()
              : "Unknown"
          }
        />
        <DetailRow
          label="Folders (including nested)"
          value={
            totalFolders !== undefined && totalFolders !== null
              ? totalFolders.toLocaleString()
              : "Unknown"
          }
        />
      </Stack>

      <Divider />

      <Box
        className="custom-scrollbar"
        sx={{
          maxHeight: 360,
          overflowY: "auto",
          pr: 1,
        }}
      >
        <Stack spacing={1}>
          {multiItems.map((item) => {
            const isDir = item.type === "directory";
            return (
              <Box
                key={item.path}
                sx={{
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: 1.5,
                  p: 1.5,
                  display: "flex",
                  flexDirection: "column",
                  gap: 0.5,
                }}
              >
                <Box
                  sx={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    gap: 2,
                  }}
                >
                  <Box>
                    <Typography variant="subtitle1" fontWeight={600}>
                      {item.name}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      {isDir ? "Directory" : "File"}
                    </Typography>
                  </Box>
                  {!isDir && (
                    <Button
                      size="small"
                      startIcon={<DownloadIcon fontSize="small" />}
                      onClick={() => onDownload(item.path)}
                    >
                      Download
                    </Button>
                  )}
                </Box>
                <Typography variant="body2" color="text.secondary">
                  Size: {formatFileSize(item.size)}
                </Typography>
                {isDir && (
                  <Typography variant="body2" color="text.secondary">
                    {`${item.fileCount ?? 0} file${item.fileCount === 1 ? "" : "s"}, ${item.folderCount ?? 0} folder${item.folderCount === 1 ? "" : "s"}`}
                  </Typography>
                )}
              </Box>
            );
          })}
        </Stack>
      </Box>
    </Paper>
  );
};

export default MultiFileDetail;
