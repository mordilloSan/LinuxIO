import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { linuxio, CACHE_TTL_MS } from "@/api";
import UpdateCard from "@/components/cards/UpdateCard";
import PageLoader from "@/components/loaders/PageLoader";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { Update } from "@/types/update";
import { getMutationErrorMessage } from "@/utils/mutations";
interface Props {
  updates: Update[];
  onUpdateClick: (pkg: string) => Promise<void>;
  isUpdating?: boolean;
  currentPackage?: string | null;
  isLoading?: boolean;
}
const UpdateList: React.FC<Props> = ({
  updates,
  onUpdateClick,
  isUpdating,
  currentPackage,
  isLoading,
}) => {
  const queryClient = useQueryClient();
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  const [changelogs, setChangelogs] = useState<Record<string, string>>({});
  const [loadingChangelog, setLoadingChangelog] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const handleFetchChangelog = useCallback(
    async (packageId: string) => {
      if (changelogs[packageId]) return; // Already loaded

      setLoadingChangelog(packageId);
      try {
        const detail = await queryClient.fetchQuery(
          linuxio.dbus.get_update_detail.queryOptions(packageId, {
            staleTime: CACHE_TTL_MS.FIVE_MINUTES,
          }),
        );
        setChangelogs((prev) => ({
          ...prev,
          [packageId]: detail.changelog || "No changelog available",
        }));
      } catch (error) {
        setChangelogs((prev) => ({
          ...prev,
          [packageId]: "Failed to load changelog",
        }));
        toast.error(getMutationErrorMessage(error, "Failed to load changelog"));
      } finally {
        setLoadingChangelog(null);
      }
    },
    [changelogs, queryClient],
  );
  const toggleExpanded = (index: number, packageId: string) => {
    if (index === expandedIdx) {
      setExpandedIdx(null);
    } else {
      setExpandedIdx(index);
      handleFetchChangelog(packageId);
    }
  };
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setExpandedIdx(null);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);
  if (isLoading) {
    return <PageLoader />;
  }
  if (!updates.length && !isUpdating) {
    return (
      <div
        style={{
          textAlign: "left",
        }}
      >
        <AppTypography variant="h6">Your system is up to date </AppTypography>
      </div>
    );
  }
  if (isUpdating) {
    return null; // Hide list while updating; only the progress bar should show
  }
  return (
    <AppGrid
      container
      spacing={2}
      style={{
        paddingBottom: 16,
      }}
      ref={containerRef}
    >
      {updates.map((update, idx) => (
        <AppGrid key={idx} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
          <UpdateCard
            update={update}
            isExpanded={expandedIdx === idx}
            isUpdating={!!isUpdating}
            isCurrentPackage={currentPackage === update.package_id}
            changelog={changelogs[update.package_id]}
            isLoadingChangelog={loadingChangelog === update.package_id}
            onToggleChangelog={() => toggleExpanded(idx, update.package_id)}
            onUpdate={() => onUpdateClick(update.package_id)}
          />
        </AppGrid>
      ))}
    </AppGrid>
  );
};
export default UpdateList;
