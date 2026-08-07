import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { CACHE_TTL_MS, linuxio, type Update } from "@/api";
import UpdateCard from "@/components/cards/UpdateCard";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
interface Props {
  currentPackage?: string | null;
  isUpdating?: boolean;
  onUpdateClick: (pkg: string) => void;
  updates: Update[];
}
const UpdateList = ({
  updates,
  onUpdateClick,
  isUpdating,
  currentPackage,
}: Props) => {
  const [expandedPackageId, setExpandedPackageId] = useState<string | null>(
    null,
  );
  // Kept across collapse so the changelog stays visible during the collapse
  // animation; expanding a card drives the fetch.
  const [changelogPackageId, setChangelogPackageId] = useState<string | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);
  const changelogQuery = useQuery(
    linuxio.updates.get_update_detail.queryOptions(changelogPackageId ?? "", {
      enabled: changelogPackageId !== null,
      staleTime: CACHE_TTL_MS.FIVE_MINUTES,
      select: (detail) => detail.changelog || "No changelog available",
    }),
  );
  const changelog = changelogQuery.isError
    ? "Failed to load changelog"
    : changelogQuery.data;
  const toggleExpanded = (packageId: string) => {
    if (packageId === expandedPackageId) {
      setExpandedPackageId(null);
    } else {
      setExpandedPackageId(packageId);
      setChangelogPackageId(packageId);
    }
  };
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setExpandedPackageId(null);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);
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
      {updates.map((update) => (
        <AppGrid key={update.package_id} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
          <UpdateCard
            changelog={
              update.package_id === changelogPackageId ? changelog : undefined
            }
            isCurrentPackage={currentPackage === update.package_id}
            isExpanded={expandedPackageId === update.package_id}
            isLoadingChangelog={
              update.package_id === changelogPackageId &&
              changelogQuery.isLoading
            }
            isUpdating={!!isUpdating}
            onToggleChangelog={() => toggleExpanded(update.package_id)}
            onUpdate={() => onUpdateClick(update.package_id)}
            update={update}
          />
        </AppGrid>
      ))}
    </AppGrid>
  );
};
export default UpdateList;
