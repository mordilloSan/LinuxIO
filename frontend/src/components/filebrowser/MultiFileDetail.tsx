import { Icon } from "@iconify/react";
import { Button, useTheme } from "@mui/material";
import React from "react";

import { MultiStatsItem } from "../../types/filebrowser";

import AppDivider from "@/components/ui/AppDivider";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
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
  const theme = useTheme();

  return (
    <div
      style={{
        display: "flex",
        gap: theme.spacing(2),
      }}
    >
      <AppTypography
        variant="body2"
        fontWeight={600}
        color="text.secondary"
        style={{ minWidth: 140 }}
      >
        {label}:
      </AppTypography>
      {isLoading ? (
        <AppTypography
          variant="body2"
          style={{
            flex: 1,
            wordBreak: "break-all",
            animation: "detailGlow 2.5s infinite",
          }}
        >
          —
        </AppTypography>
      ) : (
        <AppTypography
          component="div"
          variant="body2"
          style={{ flex: 1, wordBreak: "break-all" }}
        >
          {value}
        </AppTypography>
      )}
    </div>
  );
};

const MultiFileItemRow: React.FC<{
  item: MultiFileDetailItem;
  onDownload: (path: string) => void;
}> = ({ item, onDownload }) => {
  const theme = useTheme();
  const baseBorderRadius =
    typeof theme.shape.borderRadius === "number"
      ? theme.shape.borderRadius
      : Number.parseFloat(theme.shape.borderRadius);
  const isDir = item.type === "directory";
  const isLoading = item.isLoading ?? false;
  const [hovered, setHovered] = React.useState(false);

  const renderSize = () => {
    if (isLoading) {
      return <span style={{ animation: "detailGlow 2.5s infinite" }}>—</span>;
    }
    if (item.error) {
      return "—";
    }
    if (isDir && item.aggregateSize !== undefined) {
      return formatFileSize(item.aggregateSize);
    }
    return formatFileSize(item.size);
  };

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        border: "1px solid",
        borderColor: hovered
          ? "color-mix(in srgb, var(--mui-palette-primary-main), transparent 60%)"
          : theme.palette.divider,
        borderRadius: baseBorderRadius * 1.5,
        padding: theme.spacing(1.5),
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(0.5),
        backgroundColor: hovered
          ? "color-mix(in srgb, var(--mui-palette-primary-main), transparent 95%)"
          : "transparent",
        transition: "all 120ms ease",
        transform: hovered ? "translateY(-1px)" : "none",
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: theme.spacing(2),
        }}
      >
        <div>
          <AppTypography variant="subtitle1" fontWeight={600}>
            {item.name}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary">
            {isDir ? "Directory" : "File"}
          </AppTypography>
        </div>
        {!isDir && (
          <Button
            size="small"
            startIcon={<Icon icon="mdi:download" width={18} height={18} />}
            onClick={() => onDownload(item.path)}
          >
            Download
          </Button>
        )}
      </div>
      <AppTypography variant="body2" color="text.secondary">
        Size: {renderSize()}
      </AppTypography>
    </div>
  );
};

const MultiFileDetail: React.FC<MultiFileDetailProps> = ({
  multiItems,
  onDownload,
  totalSize,
  isLoadingDetails,
}) => {
  const theme = useTheme();

  if (!multiItems?.length) {
    return null;
  }

  return (
    <AppPaper
      variant="outlined"
      style={{
        borderRadius: 8,
        display: "flex",
        flexDirection: "column",
        padding: 12,
        gap: 8,
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: theme.spacing(2),
        }}
      >
        <div>
          <AppTypography variant="h6" fontWeight={600}>
            {multiItems.length} Selected Item
            {multiItems.length === 1 ? "" : "s"}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary">
            Combined statistics for the selected files and folders
          </AppTypography>
        </div>
      </div>

      <AppDivider />

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        <DetailRow
          label="Selected Items"
          value={multiItems.length.toLocaleString()}
        />
        <DetailRow
          label="Total Size"
          value={formatFileSize(totalSize)}
          isLoading={isLoadingDetails}
        />
      </div>

      <AppDivider />

      <div
        className="custom-scrollbar"
        style={{
          maxHeight: 360,
          overflowY: "auto",
          paddingRight: theme.spacing(1),
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(1),
          }}
        >
          {multiItems.map((item) => {
            return (
              <MultiFileItemRow
                key={item.path}
                item={item}
                onDownload={onDownload}
              />
            );
          })}
        </div>
      </div>
    </AppPaper>
  );
};

export default MultiFileDetail;
