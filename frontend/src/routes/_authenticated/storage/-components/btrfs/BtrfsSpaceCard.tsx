import { Icon } from "@iconify/react";
import { useState } from "react";

import type { BtrfsFilesystem } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppCollapse from "@/components/ui/AppCollapse";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { formatFileSize } from "@/utils/formatters";

const capitalize = (word: string) =>
  word.charAt(0).toUpperCase() + word.slice(1);

/** "data+metadata" (mixed block groups) reads "Data and metadata". */
const kindLabel = (kind: string) => capitalize(kind.split("+").join(" and "));

/**
 * What the pool stores and about how much room is left, each byte counted
 * once; how btrfs laid the disks out stays behind Details.
 */
const BtrfsSpaceCard = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const [showDetails, setShowDetails] = useState(false);
  const { storedBytes, freeBytes } = filesystem;
  const known = filesystem.allocation.length > 0;
  const sizeBytes = storedBytes + freeBytes;
  const detailsId = `btrfs-space-details-${filesystem.uuid}`;

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          iconTint
          icon={<Icon height={24} icon="mdi:chart-donut" width={24} />}
          title="Space"
        />
        {known ? (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--app-space-8)",
            }}
          >
            <div
              style={{
                alignItems: "baseline",
                display: "flex",
                flexWrap: "wrap",
                gap: "var(--app-space-8)",
              }}
            >
              <AppTypography component="p" fontWeight={700} variant="h4">
                {formatFileSize(storedBytes)}
              </AppTypography>
              <AppTypography color="text.secondary" variant="body2">
                stored
              </AppTypography>
            </div>
            <AppTypography variant="body2">
              {`About ${formatFileSize(freeBytes)} free of ${formatFileSize(sizeBytes)}`}
            </AppTypography>
            <AppLinearProgress
              aria-label="Space used"
              value={sizeBytes > 0 ? (storedBytes / sizeBytes) * 100 : 0}
              variant="determinate"
            />
          </div>
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            Space could not be read
          </AppTypography>
        )}
        {known && (
        <div style={{ marginTop: "var(--app-space-12)" }}>
          <AppButton
            aria-controls={detailsId}
            aria-expanded={showDetails}
            keepTextOnMobile
            onClick={() => setShowDetails((open) => !open)}
            size="small"
            startIcon={
              <Icon
                height={18}
                icon={showDetails ? "mdi:chevron-down" : "mdi:chevron-right"}
                width={18}
              />
            }
            variant="text"
          >
            Details: how the disk is laid out
          </AppButton>
          <div id={detailsId}>
            <AppCollapse in={showDetails} unmountOnExit>
              {filesystem.allocation.map((a) => (
                <InfoRow
                  key={`${a.kind}-${a.profile}`}
                  label={`${kindLabel(a.kind)} · ${a.profile}`}
                  noBorder
                >
                  {`${formatFileSize(a.usedBytes)} used of ${formatFileSize(a.totalBytes)} set aside`}
                </InfoRow>
              ))}
              <InfoRow label="Not set aside yet" noBorder>
                {formatFileSize(filesystem.unallocatedBytes)}
              </InfoRow>
              <InfoRow label="UUID" noBorder>
                {filesystem.uuid}
              </InfoRow>
            </AppCollapse>
          </div>
        </div>
        )}
      </AppCardContent>
    </FrostedCard>
  );
};

export default BtrfsSpaceCard;
