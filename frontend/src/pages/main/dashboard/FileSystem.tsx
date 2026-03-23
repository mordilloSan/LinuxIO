import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import MetricBar from "@/components/gauge/MetricBar";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useAppTheme } from "@/theme";
import { FilesystemInfo } from "@/types/fs";
import { formatFileSize } from "@/utils/formaters";

const FsInfoCard: React.FC = () => {
  const { data: fsInfo, isPending } = linuxio.system.get_fs_info.useQuery({
    refetchInterval: 2000,
  });
  const theme = useAppTheme();

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
          <div key={index}>
            <MetricBar
              label={fs.mountpoint}
              percent={usedPercent}
              color={theme.palette.primary.main}
              tooltip={`Free: ${formatFileSize(fs.free)} / Total: ${formatFileSize(fs.total)}`}
              rightLabel={
                <>
                  {formatFileSize(fs.used)}&nbsp;/&nbsp;
                  {formatFileSize(fs.total)}
                </>
              }
            />
          </div>
        );
      });
  };

  const data = {
    title: "FileSystems",
    stats: (
      <div style={{ width: "100%" }}>
        {isPending ? <ComponentLoader /> : renderFsProgressBars()}
      </div>
    ),
    avatarIcon: "eos-icons:file-system",
  };

  return <DashboardCard {...data} />;
};

export default FsInfoCard;
