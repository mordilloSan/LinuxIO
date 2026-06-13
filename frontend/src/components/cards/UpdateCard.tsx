import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppCardContent from "@/components/ui/AppCardContent";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTypography from "@/components/ui/AppTypography";
import { Update } from "@/types/update";

export interface UpdateCardProps {
  changelog: string | undefined;
  isCurrentPackage: boolean;
  isExpanded: boolean;
  isLoadingChangelog: boolean;
  isUpdating: boolean;
  onToggleChangelog: () => void;
  onUpdate: () => Promise<void>;
  update: Update;
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
          noWrap
          style={{
            maxWidth: "90%",
          }}
          variant="h6"
        >
          {update.summary}
        </AppTypography>
      </div>

      {/* Package & version */}
      <AppTypography
        color="text.secondary"
        gutterBottom
        noWrap
        style={{
          maxWidth: "90%",
        }}
        variant="body2"
      >
        Package: {update.package_id}
      </AppTypography>
      <AppTypography
        color="text.secondary"
        gutterBottom
        noWrap
        style={{
          maxWidth: "90%",
        }}
        variant="body2"
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
          onClick={onToggleChangelog}
          size="small"
          variant="outlined"
        />
        <Chip
          disabled={isUpdating}
          label={
            isCurrentPackage ? <AppCircularProgress size={16} /> : "Update"
          }
          onClick={onUpdate}
          size="small"
          variant="outlined"
        />
      </div>

      {/* Changelog */}
      <AppCollapse in={isExpanded} unmountOnExit>
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
            <AppTypography color="text.secondary" variant="body2">
              {changelog || "Loading..."}
            </AppTypography>
          )}
        </div>
      </AppCollapse>
    </AppCardContent>
  </FrostedCard>
);

export default UpdateCard;
