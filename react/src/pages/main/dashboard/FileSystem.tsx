import { Typography, LinearProgress, Box, Tooltip } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { FilesystemInfo } from "@/types/fs";
import axios from "@/utils/axios";

const FsInfoCard: React.FC = () => {
  const {
    data: fsInfo,
    isPending,
    isError,
  } = useQuery<FilesystemInfo[]>({
    queryKey: ["fsInfo"],
    queryFn: async () => {
      const response = await axios.get("/system/fs");
      return response.data;
    },
    refetchInterval: 2000,
  });

  const isRelevantMount = (fs: FilesystemInfo): boolean => {
    const mount = fs.mountpoint;

    return (
      fs.total > 0 &&
      mount !== "" &&
      !mount.startsWith("/var/lib/docker/") &&
      !mount.startsWith("/sys/firmware/") &&
      !mount.startsWith("/dev") &&
      !mount.startsWith("/run") &&
      !mount.startsWith("/proc") &&
      !mount.startsWith("/sys/fs")
    );
  };

  const renderFsProgressBars = () => {
    if (!fsInfo || fsInfo.length === 0) {
      return "No system information available.";
    }

    return fsInfo
      .filter((fs) => isRelevantMount(fs))
      .map((fs, index) => {
        const usedPercent = fs.usedPercent ?? 0;

        return (
          <Box key={index}>
            <Box
              display="flex"
              justifyContent="space-between"
              alignItems="center"
              mt={1.5}
            >
              <Typography variant="body2">{fs.mountpoint}</Typography>
              <Tooltip
                title={`Free: ${formatBytes(fs.free)} / Total: ${formatBytes(
                  fs.total,
                )}`}
                placement="top"
                arrow
                slotProps={{
                  popper: {
                    modifiers: [
                      { name: "offset", options: { offset: [0, -30] } },
                    ],
                    sx: { pointerEvents: "none" },
                  },
                  tooltip: {
                    sx: { pointerEvents: "auto" },
                  },
                }}
              >
                <Typography variant="body2" sx={{ cursor: "pointer" }}>
                  {formatBytes(fs.used)} of {formatBytes(fs.total)}
                </Typography>
              </Tooltip>
            </Box>
            <LinearProgress
              variant="determinate"
              value={usedPercent}
              sx={{
                height: 2,
                borderRadius: 1,
              }}
            />
          </Box>
        );
      });
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
  };

  const data = {
    title: "FileSystems",
    stats: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      renderFsProgressBars()
    ),
    avatarIcon: "eos-icons:file-system",
  };

  return <GeneralCard {...data} />;
};

export default FsInfoCard;
