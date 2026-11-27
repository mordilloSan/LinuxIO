import type { Update } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import SelectableCard from "@/components/cards/SelectableCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_LG } from "@/theme/constants";

import { isDeferredUpdate, updatePackageName } from "./packageUpdates";

// About the height the card had with its action row, so a card in the grid
// and the focused one, which has the Update action, are the same size.
const UPDATE_CARD_MIN_HEIGHT = 120;

interface UpdateCardProps {
  /** Set on a card in the list: the whole card opens the update's details. */
  onOpen?: () => void;
  onPrefetchChangelog?: () => void;
  /**
   * The Update action, shown on the focused card only, since a card that
   * opens its details is a button itself.
   */
  onUpdate?: () => void;
  selected?: boolean;
  update: Update;
}

const UpdateCard = ({
  update,
  onOpen,
  onPrefetchChangelog,
  onUpdate,
  selected = false,
}: UpdateCardProps) => {
  const isDeferred = isDeferredUpdate(update);
  const packageName = updatePackageName(update.package_id);
  // The version is in the details panel's header, not on the card.
  const subtitle = update.summary.trim();

  return (
    <SelectableCard label={`update ${packageName}`} onOpen={onOpen}>
      <FrostedCard
        accent
        hoverLift
        onMouseEnter={onPrefetchChangelog}
        style={{
          boxSizing: "border-box",
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: UPDATE_CARD_MIN_HEIGHT,
          minWidth: 0,
          padding: CARD_PADDING_LG,
          width: "100%",
          ...(selected && {
            borderBottomColor: "var(--fc-accent)",
            boxShadow: "var(--fc-lift-shadow)",
          }),
        }}
      >
        <div
          style={{
            alignItems: "flex-start",
            display: "flex",
            gap: 8,
            justifyContent: "space-between",
          }}
        >
          <div style={{ minWidth: 0 }}>
            <AppTypography
              component="div"
              fontWeight={700}
              noWrap
              style={{ lineHeight: 1.25 }}
              variant="subtitle1"
            >
              {packageName}
            </AppTypography>
            {subtitle ? (
              // A block, so noWrap's ellipsis applies; on the inline span a
              // caption renders as, the text ran past the card's padding.
              <AppTypography
                color="text.secondary"
                component="div"
                noWrap
                variant="caption"
              >
                {subtitle}
              </AppTypography>
            ) : null}
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
          <AppTypography
            color="text.secondary"
            style={{ marginTop: 12 }}
            variant="body2"
          >
            This update is currently deferred by PackageKit. This commonly
            happens during phased rollouts; try again later.
          </AppTypography>
        ) : null}

        {!onOpen && onUpdate ? (
          <div
            style={{
              display: "flex",
              gap: 8,
              marginTop: "auto",
              paddingTop: 16,
            }}
          >
            <AppButton
              disabled={isDeferred}
              onClick={onUpdate}
              size="small"
              variant="outlined"
            >
              Update
            </AppButton>
          </div>
        ) : null}
      </FrostedCard>
    </SelectableCard>
  );
};

export default UpdateCard;
