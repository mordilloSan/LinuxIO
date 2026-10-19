import { Icon } from "@iconify/react";

import type { BtrfsFilesystem, BtrfsScrubStatus } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import InfoRow from "@/components/ui/InfoRow";
import { formatDate } from "@/utils/formaters";

import { deviceErrorCount } from "./BtrfsDevicesCard";

const scrubSummary = (scrub: BtrfsScrubStatus) => {
  switch (scrub.status) {
    case "none":
      return "Never scrubbed";
    case "running":
      return `Scrub running · ${scrub.percent}%`;
    default:
      return `Last scrub: ${formatDate(scrub.finishedAt)}, ${scrub.errorCount} errors`;
  }
};

const BtrfsHealthCard = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const hasErrors = filesystem.devices.some((d) => deviceErrorCount(d) > 0);
  const status = hasErrors
    ? "Errors"
    : filesystem.degraded
      ? "Degraded"
      : "Healthy";

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:heart-pulse" width={24} />}
          right={
            <AppChip
              color={status === "Healthy" ? "success" : "error"}
              label={status}
              size="small"
              variant="soft"
            />
          }
          title="Health"
        />
        <InfoRow label="Scrub">{scrubSummary(filesystem.scrub)}</InfoRow>
        <InfoRow label="UUID" noBorder>
          {filesystem.uuid}
        </InfoRow>
      </AppCardContent>
    </FrostedCard>
  );
};

export default BtrfsHealthCard;
