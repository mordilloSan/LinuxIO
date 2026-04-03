import React from "react";

import FrostedCard from "@/components/cards/RootCard";
import AppCardContent from "@/components/ui/AppCardContent";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTypography from "@/components/ui/AppTypography";
import { Update } from "@/types/update";

export interface UpdateCardProps {
  update: Update;
  isExpanded: boolean;
  isUpdating: boolean;
  isCurrentPackage: boolean;
  changelog: string | undefined;
  isLoadingChangelog: boolean;
  onToggleChangelog: () => void;
  onUpdate: () => Promise<void>;
}

const UpdateCard: React.FC<UpdateCardProps> = ({
  update,
  isExpanded,
  isUpdating,
  isCurrentPackage,
  changelog,
  isLoadingChangelog,
  onToggleChangelog,
  onUpdate,
}) => (
  <FrostedCard hoverLift>
    <AppCardContent>
      {/* Title */}
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          marginBottom: 24,
        }}
      >
        <AppTypography
          variant="h6"
          style={{
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
            maxWidth: "90%",
          }}
        >
          {update.summary}
        </AppTypography>
      </div>

      {/* Package & version */}
      <AppTypography
        variant="body2"
        color="text.secondary"
        gutterBottom
        style={{
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
          maxWidth: "90%",
        }}
      >
        Package: {update.package_id}
      </AppTypography>
      <AppTypography
        variant="body2"
        color="text.secondary"
        gutterBottom
        style={{
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
          maxWidth: "90%",
        }}
      >
        Version: {update.version}
      </AppTypography>

      {/* Actions */}
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 8,
          marginTop: 12,
        }}
      >
        <Chip
          label="View Changelog"
          size="small"
          variant="outlined"
          onClick={onToggleChangelog}
        />
        <Chip
          label={
            isCurrentPackage ? <AppCircularProgress size={16} /> : "Update"
          }
          size="small"
          variant="outlined"
          disabled={isUpdating}
          onClick={onUpdate}
        />
      </div>

      {/* Changelog */}
      <AppCollapse in={isExpanded} timeout="auto" unmountOnExit>
        <div style={{ whiteSpace: "pre-wrap", fontSize: 14, marginTop: 32 }}>
          {isLoadingChangelog ? (
            <div
              style={{
                display: "flex",
                justifyContent: "center",
                paddingTop: 16,
                paddingBottom: 16,
              }}
            >
              <AppCircularProgress size={20} />
            </div>
          ) : (
            <AppTypography variant="body2" color="text.secondary">
              {changelog || "Loading..."}
            </AppTypography>
          )}
        </div>
      </AppCollapse>
    </AppCardContent>
  </FrostedCard>
);

export default UpdateCard;
