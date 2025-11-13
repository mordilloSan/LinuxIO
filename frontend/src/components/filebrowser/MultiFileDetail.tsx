import DownloadIcon from "@mui/icons-material/Download";
import { Box, Button, Divider, Paper, Stack, Typography } from "@mui/material";
import { alpha, useTheme } from "@mui/material/styles";
import React from "react";

import { MultiStatsItem } from "../../types/filebrowser";

import { formatFileSize } from "@/utils/formaters";

// Glow animation for loading states
const glowAnimation = `
  @keyframes detailGlow {
    0% { opacity: 0.5; }
    25% { opacity: 0.7; }
    50% { opacity: 1; }
    75% { opacity: 0.7; }
    100% { opacity: 0.5; }
  }
`;

// Inject glow animation styles once
if (
  typeof document !== "undefined" &&
  !document.getElementById("detailGlowStyles")
) {
  const style = document.createElement("style");
  style.id = "detailGlowStyles";
  style.textContent = glowAnimation;
  document.head.appendChild(style);
}

interface MultiFileDetailItem extends MultiStatsItem {
  isLoading?: boolean;
  error?: Error | null;
}

interface MultiFileDetailProps {
  multiItems: MultiFileDetailItem[];
  onDownload: (path: string) => void;
  totalSize?: number | null;
  isLoadingDetails?: boolean;
}

const DetailRow: React.FC<{
  label: string;
  value: React.ReactNode;
  isLoading?: boolean;
}> = ({ label, value, isLoading = false }) => {
  return (
    <Box sx={{ display: "flex", gap: 2 }}>
      <Typography
        variant="body2"
        fontWeight={600}
        color="text.secondary"
        sx={{ minWidth: 140 }}
      >
        {label}:
      </Typography>
      {isLoading ? (
        <Typography
          variant="body2"
          sx={{
            flex: 1,
            wordBreak: "break-all",
            animation: "detailGlow 2.5s infinite",
          }}
        >
          —
        </Typography>
      ) : (
        <Typography variant="body2" sx={{ flex: 1, wordBreak: "break-all" }}>
          {value}
        </Typography>
      )}
    </Box>
  );
};

const MultiFileItemRow: React.FC<{
  item: MultiFileDetailItem;
  onDownload: (path: string) => void;
}> = ({ item, onDownload }) => {
  const isDir = item.type === "directory";
  const isLoading = item.isLoading ?? false;
  const theme = useTheme();
  const [hovered, setHovered] = React.useState(false);

  return (
    <Box
      key={item.path}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      sx={{
        border: "1px solid",
        borderColor: hovered
          ? alpha(theme.palette.primary.main, 0.4)
          : "divider",
        borderRadius: 1.5,
        p: 1.5,
        display: "flex",
        flexDirection: "column",
        gap: 0.5,
        backgroundColor: hovered
          ? alpha(theme.palette.primary.main, 0.05)
          : "transparent",
        transition: "all 120ms ease",
        transform: hovered ? "translateY(-1px)" : "none",
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
        Size:{" "}
        {isLoading ? (
          <span style={{ animation: "detailGlow 2.5s infinite" }}>—</span>
        ) : isDir && (item as any).aggregateSize !== undefined ? (
          formatFileSize((item as any).aggregateSize)
        ) : (
          formatFileSize(item.size)
        )}
        {isDir && item.error && (
          <Typography
            component="span"
            variant="body2"
            color="error"
            sx={{ ml: 1, fontSize: "0.85rem" }}
          >
            ⚠ Failed to load size
          </Typography>
        )}
      </Typography>
    </Box>
  );
};

const MultiFileDetail: React.FC<MultiFileDetailProps> = ({
  multiItems,
  onDownload,
  totalSize,
  isLoadingDetails,
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
        <DetailRow
          label="Total Size"
          value={formatFileSize(totalSize)}
          isLoading={isLoadingDetails}
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
            return (
              <MultiFileItemRow
                key={item.path}
                item={item}
                onDownload={onDownload}
              />
            );
          })}
        </Stack>
      </Box>
    </Paper>
  );
};

export default MultiFileDetail;
