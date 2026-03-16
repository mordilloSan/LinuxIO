import { CardContent, CircularProgress, Collapse, Grid } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { linuxio, CACHE_TTL_MS } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import Chip from "@/components/ui/AppChip";
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
  const theme = useTheme();
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
    return <ComponentLoader />;
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
    <Grid
      container
      spacing={2}
      sx={{
        px: 2,
        pb: 2,
      }}
      ref={containerRef}
    >
      {updates.map((update, idx) => (
        <Grid
          key={idx}
          size={{
            xs: 12,
            sm: 4,
            md: 4,
            lg: 3,
            xl: 2,
          }}
        >
          <FrostedCard hoverLift>
            <CardContent>
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  marginBottom: theme.spacing(3),
                }}
              >
                <AppTypography
                  variant="h6"
                  style={{
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    maxWidth: "90%",
                  }}
                >
                  {update.summary}
                </AppTypography>
              </div>

              <AppTypography
                variant="body2"
                color="text.secondary"
                gutterBottom
                style={{
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  maxWidth: "90%",
                }}
              >
                Package: {update.package_id}
              </AppTypography>

              <AppTypography
                variant="body2"
                color="text.secondary"
                gutterBottom
                style={{
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  maxWidth: "90%",
                }}
              >
                Version: {update.version}
              </AppTypography>

              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: theme.spacing(3),
                  marginTop: theme.spacing(4),
                  marginBottom: `-${theme.spacing(2)}`,
                }}
              >
                <Chip
                  label="View Changelog"
                  size="small"
                  variant="outlined"
                  onClick={() => toggleExpanded(idx, update.package_id)}
                  sx={{
                    cursor: "pointer",
                  }}
                />
                <Chip
                  label={
                    currentPackage === update.package_id ? (
                      <CircularProgress size={16} />
                    ) : (
                      "Update"
                    )
                  }
                  size="small"
                  variant="outlined"
                  disabled={!!isUpdating}
                  onClick={async () => {
                    await onUpdateClick(update.package_id);
                  }}
                  sx={{
                    cursor: "pointer",
                  }}
                />
              </div>

              <Collapse in={expandedIdx === idx} timeout="auto" unmountOnExit>
                <div
                  style={{
                    whiteSpace: "pre-wrap",
                    fontSize: 14,
                    marginTop: theme.spacing(4),
                  }}
                >
                  {loadingChangelog === update.package_id ? (
                    <div
                      style={{
                        display: "flex",
                        justifyContent: "center",
                        paddingTop: theme.spacing(2),
                        paddingBottom: theme.spacing(2),
                      }}
                    >
                      <CircularProgress size={20} />
                    </div>
                  ) : (
                    <AppTypography variant="body2" color="text.secondary">
                      {changelogs[update.package_id] || "Loading..."}
                    </AppTypography>
                  )}
                </div>
              </Collapse>
            </CardContent>
          </FrostedCard>
        </Grid>
      ))}
    </Grid>
  );
};
export default UpdateList;
