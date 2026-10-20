import { Icon } from "@iconify/react";

import type { BtrfsFilesystem } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCardContent from "@/components/ui/AppCardContent";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import InfoRow from "@/components/ui/InfoRow";
import { formatFileSize } from "@/utils/formaters";

const BtrfsSpaceCard = ({ filesystem }: { filesystem: BtrfsFilesystem }) => (
  <FrostedCard>
    <AppCardContent>
      <CardIconHeader
        iconTint
        icon={<Icon height={24} icon="mdi:chart-donut" width={24} />}
        subtitle={`${formatFileSize(filesystem.usedBytes)} used of ${formatFileSize(filesystem.totalBytes)}`}
        title="Space"
      />
      {filesystem.allocation.map((a) => (
        <div key={`${a.kind}-${a.profile}`}>
          <InfoRow label={`${a.kind} · ${a.profile}`} noBorder>
            {formatFileSize(a.usedBytes)} / {formatFileSize(a.totalBytes)}
          </InfoRow>
          <AppLinearProgress
            aria-label={`${a.kind} ${a.profile} usage`}
            value={a.totalBytes > 0 ? (a.usedBytes / a.totalBytes) * 100 : 0}
            variant="determinate"
          />
        </div>
      ))}
      <InfoRow label="Unallocated">
        {formatFileSize(filesystem.unallocatedBytes)}
      </InfoRow>
      <InfoRow label="UUID" noBorder>
        {filesystem.uuid}
      </InfoRow>
    </AppCardContent>
  </FrostedCard>
);

export default BtrfsSpaceCard;
