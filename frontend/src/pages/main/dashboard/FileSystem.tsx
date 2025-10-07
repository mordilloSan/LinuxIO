import { Box } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import MetricBar from "@/components/gauge/MetricBar";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { FilesystemInfo } from "@/types/fs";
import axios from "@/utils/axios";
import { formatBytes } from "@/utils/formatBytes";

const FsInfoCard: React.FC = () => {
  const { data: fsInfo, isPending } = useQuery<FilesystemInfo[]>({
    queryKey: ["fsInfo"],
    queryFn: async () => {
      const response = await axios.get("/system/fs");
      return response.data;
    },
    refetchInterval: 2000,
  });
  const theme = useTheme();
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
            <MetricBar
              label={fs.mountpoint}
              percent={usedPercent}
              color={theme.palette.primary.main}
              tooltip={`Free: ${formatBytes(fs.free)} / Total: ${formatBytes(fs.total)}`}
              rightLabel={
                <>
                  {formatBytes(fs.used)}&nbsp;/&nbsp;{formatBytes(fs.total)}
                </>
              }
            />
          </Box>
        );
      });
  };

  const data = {
    title: "FileSystems",
    stats: isPending ? <ComponentLoader /> : renderFsProgressBars(),
    avatarIcon: "eos-icons:file-system",
  };

  return <GeneralCard {...data} />;
};

export default FsInfoCard;
