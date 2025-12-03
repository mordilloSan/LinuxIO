import CloseIcon from "@mui/icons-material/Close";
import { Box, IconButton, LinearProgress, Typography } from "@mui/material";
import React from "react";

import { useFileTransfers } from "@/hooks/useFileTransfers";

const DownloadNotifications: React.FC = () => {
  const { transfers, cancelDownload, cancelUpload } = useFileTransfers();

  if (transfers.length === 0) return null;

  return (
    <Box
      sx={{
        position: "fixed",
        bottom: 16,
        right: 16,
        zIndex: 1400,
        display: "flex",
        flexDirection: "column",
        gap: 2,
        maxWidth: 320,
      }}
    >
      {transfers.map((transfer) => (
        <Box
          key={transfer.id}
          sx={{
            bgcolor: "background.paper",
            boxShadow: 6,
            borderRadius: 2,
            p: 2,
            border: "1px solid",
            borderColor: "divider",
          }}
        >
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              mb: 1,
            }}
          >
            <Typography variant="subtitle2" fontWeight="bold">
              {transfer.type === "download" ? "Download" : "Upload"} Progress
            </Typography>
            <IconButton
              size="small"
              onClick={() =>
                transfer.type === "download"
                  ? cancelDownload(transfer.id)
                  : cancelUpload(transfer.id)
              }
              sx={{ ml: 1 }}
            >
              <CloseIcon fontSize="small" />
            </IconButton>
          </Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {transfer.label ||
              (transfer.type === "download"
                ? "Preparing archive..."
                : "Preparing upload...")}
          </Typography>
          <LinearProgress
            variant="determinate"
            value={transfer.progress}
            sx={{ height: 6, borderRadius: 1 }}
          />
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ mt: 0.5, display: "block" }}
          >
            {transfer.progress}%
          </Typography>
        </Box>
      ))}
    </Box>
  );
};

export default DownloadNotifications;
