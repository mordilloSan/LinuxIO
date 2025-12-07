import CloseIcon from "@mui/icons-material/Close";
import { Box, IconButton, LinearProgress, Typography } from "@mui/material";
import React from "react";

import { useFileTransfers } from "@/hooks/useFileTransfers";

const DownloadNotifications: React.FC = () => {
  const { transfers, cancelDownload, cancelUpload, cancelCompression } =
    useFileTransfers();
  const [isExpanded, setIsExpanded] = React.useState(false);
  const hasTransfers = transfers.length > 0;

  const leastProgressTransfer = React.useMemo(() => {
    if (!hasTransfers) return null;
    return transfers.reduce(
      (lowest, current) =>
        current.progress < lowest.progress ? current : lowest,
      transfers[0],
    );
  }, [hasTransfers, transfers]);

  React.useEffect(() => {
    if (!hasTransfers && isExpanded) {
      setIsExpanded(false);
    }
  }, [hasTransfers, isExpanded]);

  if (!hasTransfers || !leastProgressTransfer) return null;

  const getTitle = (type: (typeof transfers)[number]["type"]) => {
    switch (type) {
      case "download":
        return "Download Progress";
      case "upload":
        return "Upload Progress";
      case "compression":
        return "Compression Progress";
      default:
        return "Progress";
    }
  };

  const handleCancel = (transfer: (typeof transfers)[number]) => {
    if (transfer.type === "download") {
      cancelDownload(transfer.id);
    } else if (transfer.type === "upload") {
      cancelUpload(transfer.id);
    } else if (transfer.type === "compression") {
      cancelCompression(transfer.id);
    }
  };

  return (
    <Box sx={{ position: "relative", display: "inline-flex" }}>
      <Box
        role="button"
        tabIndex={0}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setIsExpanded((prev) => !prev);
          }
        }}
        onClick={() => setIsExpanded((prev) => !prev)}
        sx={{
          cursor: "pointer",
          display: "flex",
          flexDirection: "column",
          gap: 0.5,
          border: "1px solid",
          borderColor: isExpanded ? "primary.main" : "divider",
          borderRadius: 1,
          p: 1,
          minWidth: 180,
          maxWidth: 220,
          backgroundColor: "background.paper",
          boxShadow: isExpanded ? 4 : "none",
        }}
      >
        <Typography variant="caption" color="text.secondary">
          {getTitle(leastProgressTransfer.type)}
        </Typography>
        <LinearProgress
          variant="determinate"
          value={leastProgressTransfer.progress}
          sx={{ height: 6, borderRadius: 1 }}
        />
      </Box>
      {isExpanded && (
        <Box
          sx={{
            position: "absolute",
            bottom: "calc(100% + 12px)",
            left: 0,
            zIndex: 1400,
            maxWidth: 380,
            boxShadow: 6,
            borderRadius: 2,
            border: "1px solid",
            borderColor: "divider",
            overflow: "hidden",
            backgroundColor: "background.paper",
          }}
        >
          <Box
            sx={{
              p: 2,
              pb: 1.5,
              borderBottom: "1px solid",
              borderColor: "divider",
            }}
          >
            <Typography variant="subtitle2" fontWeight="bold">
              File Operations
            </Typography>
          </Box>
          <Box
            sx={{ p: 1.5, display: "flex", flexDirection: "column", gap: 2 }}
          >
            {transfers.map((transfer) => (
              <Box key={transfer.id}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    mb: 0.5,
                  }}
                >
                  <Typography variant="body2" fontWeight="medium">
                    {getTitle(transfer.type)}
                  </Typography>
                  <IconButton
                    size="small"
                    onClick={() => handleCancel(transfer)}
                    sx={{ ml: 1, p: 0.5 }}
                  >
                    <CloseIcon fontSize="small" />
                  </IconButton>
                </Box>
                <Typography
                  variant="caption"
                  sx={{ mb: 0.5, display: "block" }}
                >
                  {transfer.label ||
                    (transfer.type === "download"
                      ? "Preparing archive..."
                      : transfer.type === "upload"
                        ? "Preparing upload..."
                        : "Compressing selection...")}
                </Typography>
                <LinearProgress
                  variant="determinate"
                  value={transfer.progress}
                  sx={{ height: 6, borderRadius: 1 }}
                />
              </Box>
            ))}
          </Box>
        </Box>
      )}
    </Box>
  );
};

export default DownloadNotifications;
