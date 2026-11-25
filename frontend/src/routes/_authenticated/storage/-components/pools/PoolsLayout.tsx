import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { linuxio } from "@/api";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useClearSelectionOnEscape } from "@/hooks/useClearSelectionOnEscape";

import { describePool } from "../disk-overview/storageGraph";
import PoolSummaryCard from "./PoolSummaryCard";

interface PoolsLayoutProps {
  /** The pool page's panel: the selected pool's route outlet. */
  children: ReactNode;
  onSelect: (uuid: string | null) => void;
  selectedUuid: string | null;
}

/**
 * The Pools tab: the mounted pools as cards, or the selected one's card
 * beside its details, as the VM list does. Selection lives in the path.
 */
const PoolsLayout = ({
  children,
  onSelect,
  selectedUuid,
}: PoolsLayoutProps) => {
  // Polled as the Disks overview does: the health banner and the running
  // check, rebuild or balance on the pool page all come from this list.
  const { data: filesystems = [], isPending } = useQuery({
    ...linuxio.storage.btrfs_list,
    refetchInterval: 10000,
  });
  useClearSelectionOnEscape(selectedUuid !== null, () => onSelect(null));
  const pools = filesystems.map((fs, index) => ({
    fs,
    name: describePool(fs, index).name,
  }));

  if (selectedUuid) {
    const selected = pools.find(({ fs }) => fs.uuid === selectedUuid);
    if (!selected) {
      if (isPending) return null;
      return (
        <AppAlert
          action={
            <AppButton onClick={() => onSelect(null)} size="small">
              Back to pools
            </AppButton>
          }
          severity="info"
        >
          This pool is not mounted, or no longer exists.{" "}
          <Link to="/storage">Mount it from the Disks tab</Link>
        </AppAlert>
      );
    }
    return (
      <ResourceDetailsLayout
        onClose={() => onSelect(null)}
        resourceLabel="pool"
        subtitle="Health, snapshots, disks and settings"
        summary={
          <PoolSummaryCard fs={selected.fs} name={selected.name} selected />
        }
        summaryWidth="content"
        title={selected.name}
      >
        {children}
      </ResourceDetailsLayout>
    );
  }

  if (!isPending && pools.length === 0) {
    return (
      <AppTypography color="text.secondary">
        No btrfs pool is mounted. Create one on the{" "}
        <Link to="/storage">Disks</Link> tab.
      </AppTypography>
    );
  }

  return (
    <AppGrid container spacing={2}>
      {pools.map(({ fs, name }) => (
        <AppGrid key={fs.uuid} size={{ xs: 12, sm: 6, lg: 4 }}>
          <PoolSummaryCard
            fs={fs}
            name={name}
            onOpen={() => onSelect(fs.uuid)}
          />
        </AppGrid>
      ))}
    </AppGrid>
  );
};

export default PoolsLayout;
