import type { Update } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_LG } from "@/theme/constants";
import { isDeferredUpdate } from "@/utils/packageUpdates";

export interface UpdateCardProps {
  changelog: string | undefined;
  isCurrentPackage: boolean;
  isExpanded: boolean;
  isLoadingChangelog: boolean;
  isUpdating: boolean;
  onToggleChangelog: () => void;
  onUpdate: () => void;
  update: Update;
}

const UpdateCard = ({
  update,
  isExpanded,
  isUpdating,
  isCurrentPackage,
  changelog,
  isLoadingChangelog,
  onToggleChangelog,
  onUpdate,
}: UpdateCardProps) => {
  const isDeferred = isDeferredUpdate(update);
  const packageName = update.package_id.split(";")[0] || update.package_id;
  const title = update.summary.trim() || packageName;
  const subtitle = update.version
    ? `${packageName} · ${update.version}`
    : packageName;

  return (
    <FrostedCard
      accent
      hoverLift
      style={{
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        height: "100%",
        padding: CARD_PADDING_LG,
      }}
    >
      <div
        style={{
          alignItems: "flex-start",
          display: "flex",
          gap: 8,
          justifyContent: "space-between",
          marginBottom: 16,
        }}
      >
        <div style={{ minWidth: 0 }}>
          <AppTypography
            fontWeight={700}
            noWrap
            style={{ lineHeight: 1.25 }}
            variant="subtitle1"
          >
            {title}
          </AppTypography>
          <AppTypography color="text.secondary" noWrap variant="caption">
            {subtitle}
          </AppTypography>
        </div>
        {isDeferred ? (
          <Chip
            color="warning"
            label="Available later"
            size="small"
            variant="soft"
          />
        ) : null}
      </div>

      {isDeferred ? (
        <AppTypography color="text.secondary" variant="body2">
          This update is currently deferred by PackageKit. This commonly happens
          during phased rollouts; try again later.
        </AppTypography>
      ) : null}

      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 8,
          marginTop: 12,
        }}
      >
        <AppButton
          color="inherit"
          onClick={onToggleChangelog}
          size="small"
          variant="text"
        >
          View Changelog
        </AppButton>
        <AppButton
          disabled={isUpdating || isDeferred}
          onClick={onUpdate}
          size="small"
          variant="outlined"
        >
          {isCurrentPackage ? (
            <span style={{ alignItems: "center", display: "flex", gap: 6 }}>
              <AppCircularProgress color="inherit" size={14} />
              Updating
            </span>
          ) : (
            "Update"
          )}
        </AppButton>
      </div>

      <AppCollapse in={isExpanded} unmountOnExit>
        <div style={{ whiteSpace: "pre-wrap", fontSize: 14, marginTop: 16 }}>
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
    </FrostedCard>
  );
};

export default UpdateCard;
