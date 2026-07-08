import React, { useEffect, useRef, useState } from "react";

import { CACHE_TTL_MS, linuxio, type Update } from "@/api";
import UpdateCard from "@/components/cards/UpdateCard";
import PageLoader from "@/components/loaders/PageLoader";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
interface Props {
  currentPackage?: string | null;
  isLoading?: boolean;
  isUpdating?: boolean;
  onUpdateClick: (pkg: string) => void;
  updates: Update[];
}
const UpdateList = ({
  updates,
  onUpdateClick,
  isUpdating,
  currentPackage,
  isLoading,
}: Props) => {
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  // Kept across collapse so the changelog stays visible during the collapse
  // animation; expanding a card drives the fetch.
  const [changelogPackageId, setChangelogPackageId] = useState<string | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);
  const changelogQuery = linuxio.updates.get_update_detail.useQuery(
    changelogPackageId ?? "",
    {
      enabled: changelogPackageId !== null,
      staleTime: CACHE_TTL_MS.FIVE_MINUTES,
      select: (detail) => detail.changelog || "No changelog available",
    },
  );
  const changelog = changelogQuery.isError
    ? "Failed to load changelog"
    : changelogQuery.data;
  const toggleExpanded = (index: number, packageId: string) => {
    if (index === expandedIdx) {
      setExpandedIdx(null);
    } else {
      setExpandedIdx(index);
      setChangelogPackageId(packageId);
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
      ref={containerRef}
      spacing={2}
      style={{
        paddingBottom: 16,
      }}
    >
      {updates.map((update, idx) => (
        <AppGrid key={idx} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
          <UpdateCard
            changelog={
              update.package_id === changelogPackageId ? changelog : undefined
            }
            isCurrentPackage={currentPackage === update.package_id}
            isExpanded={expandedIdx === idx}
            isLoadingChangelog={
              update.package_id === changelogPackageId &&
              changelogQuery.isLoading
            }
            isUpdating={!!isUpdating}
            onToggleChangelog={() => toggleExpanded(idx, update.package_id)}
            onUpdate={() => onUpdateClick(update.package_id)}
            update={update}
          />
        </AppGrid>
      ))}
    </AppGrid>
  );
};
export default UpdateList;
