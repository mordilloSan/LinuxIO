import {
  Box,
  CardContent,
  Typography,
  Chip,
  Grid,
  Collapse,
  CircularProgress,
} from "@mui/material";
import React, { useCallback, useEffect, useRef, useState } from "react";

import { linuxio } from "@/api/linuxio";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { Update } from "@/types/update";

interface Props {
  updates: Update[];
  onUpdateClick: (pkg: string) => Promise<void>;
  isUpdating?: boolean;
  currentPackage?: string | null;
  onComplete: () => void | Promise<any>;
  isLoading?: boolean;
}

const UpdateList: React.FC<Props> = ({
  updates,
  onUpdateClick,
  isUpdating,
  currentPackage,
  onComplete,
  isLoading,
}) => {
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  const [changelogs, setChangelogs] = useState<Record<string, string>>({});
  const [loadingChangelog, setLoadingChangelog] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Mutation for fetching changelog details
  const changelogMutation = linuxio.useMutate<Update, string>(
    "dbus",
    "GetUpdateDetail",
  );

  const fetchChangelog = useCallback(
    (packageId: string) => {
      if (changelogs[packageId]) return; // Already loaded

      setLoadingChangelog(packageId);
      changelogMutation.mutate(packageId, {
        onSuccess: (detail) => {
          setChangelogs((prev) => ({
            ...prev,
            [packageId]: detail.changelog || "No changelog available",
          }));
          setLoadingChangelog(null);
        },
        onError: () => {
          setChangelogs((prev) => ({
            ...prev,
            [packageId]: "Failed to load changelog",
          }));
          setLoadingChangelog(null);
        },
      });
    },
    [changelogs, changelogMutation],
  );

  const toggleExpanded = (index: number, packageId: string) => {
    if (index === expandedIdx) {
      setExpandedIdx(null);
    } else {
      setExpandedIdx(index);
      fetchChangelog(packageId);
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
      <Box sx={{ textAlign: "left" }}>
        <Typography variant="h6">Your system is up to date </Typography>
      </Box>
    );
  }

  if (isUpdating) {
    return null; // Hide list while updating; only the progress bar should show
  }

  return (
    <Grid container spacing={2} sx={{ px: 2, pb: 2 }} ref={containerRef}>
      {updates.map((update, idx) => (
        <Grid key={idx} size={{ xs: 12, sm: 4, md: 4, lg: 3, xl: 2 }}>
          <FrostedCard
            variant="outlined"
            sx={{
              transition: "transform 0.2s, box-shadow 0.2s",
              "&:hover": {
                transform: "translateY(-4px)",
                boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
              },
            }}
          >
            <CardContent>
              <Box
                sx={{
                  display: "flex",
                  justifyContent: "space-between",
                  mb: 3,
                }}
              >
                <Typography
                  variant="h6"
                  sx={{
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    maxWidth: "90%", // or a specific px/em width
                  }}
                >
                  {update.summary}
                </Typography>
              </Box>

              <Typography
                variant="body2"
                color="text.secondary"
                gutterBottom
                sx={{
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  maxWidth: "90%", // or a specific px/em width
                }}
              >
                Package: {update.package_id}
              </Typography>

              <Typography
                variant="body2"
                color="text.secondary"
                gutterBottom
                sx={{
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  maxWidth: "90%", // or a specific px/em width
                }}
              >
                Version: {update.version}
              </Typography>

              <Box
                sx={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 3,
                  mt: 4,
                  mb: -2,
                }}
              >
                <Chip
                  label="View Changelog"
                  size="small"
                  variant="outlined"
                  onClick={() => toggleExpanded(idx, update.package_id)}
                  sx={{ cursor: "pointer" }}
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
                    await onComplete();
                  }}
                  sx={{ cursor: "pointer" }}
                />
              </Box>

              <Collapse in={expandedIdx === idx} timeout="auto" unmountOnExit>
                <Box sx={{ whiteSpace: "pre-wrap", fontSize: 14, mt: 4 }}>
                  {loadingChangelog === update.package_id ? (
                    <Box
                      sx={{ display: "flex", justifyContent: "center", py: 2 }}
                    >
                      <CircularProgress size={20} />
                    </Box>
                  ) : (
                    <Typography variant="body2" color="text.secondary">
                      {changelogs[update.package_id] || "Loading..."}
                    </Typography>
                  )}
                </Box>
              </Collapse>
            </CardContent>
          </FrostedCard>
        </Grid>
      ))}
    </Grid>
  );
};

export default UpdateList;
