import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { CACHE_TTL_MS, linuxio, type Update } from "@/api";
import UpdateCard from "@/components/cards/UpdateCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
interface Props {
  currentPackage?: string | null;
  isUpdating?: boolean;
  onUpdateClick: (pkg: string) => void;
  updates: Update[];
}
const getUpdateId = (update: Update) => update.package_id;

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
  const surface = useReorderableSurface({
    getId: getUpdateId,
    items: updates,
    surface: "updates.list",
  });
  const changelogQuery = useQuery({
    ...linuxio.updates.get_update_detail({
      packageId: changelogPackageId ?? "",
    }),
    enabled: changelogPackageId !== null,
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
    select: (detail) => detail.changelog || "No changelog available",
  });
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
    <div
      ref={containerRef}
      style={{
        display: "flex",
        flexDirection: "column",
        flex: "1 1 auto",
        minHeight: 0,
      }}
    >
      <ReorderableCardGrid
        columns={{ xs: 1, sm: 2, md: 3, lg: 4, xl: 5 }}
        fillAvailable
        getId={getUpdateId}
        renderItem={(update) => (
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
        )}
        size={1}
        surface={surface}
      />
    </div>
  );
};
export default UpdateList;
