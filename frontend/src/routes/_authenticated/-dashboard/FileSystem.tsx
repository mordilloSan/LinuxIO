import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, type FilesystemInfo } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import MetricBar from "@/components/gauge/MetricBar";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

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

const FsStats = () => {
  const { data: fsInfo } = useSuspenseQuery({
    ...linuxio.system.get_fs_info,
    refetchInterval: 2000,
  });
  const theme = useAppTheme();

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
              color={theme.palette.primary.main}
              label={fs.mountpoint}
              percent={usedPercent}
              rightLabel={
                <>
                  {formatFileSize(fs.used)}&nbsp;/&nbsp;
                  {formatFileSize(fs.total)}
                </>
              }
              tooltip={`Free: ${formatFileSize(fs.free)} / Total: ${formatFileSize(fs.total)}`}
            />
          </div>
        );
      });
  };

  return <div style={{ width: "100%" }}>{renderFsProgressBars()}</div>;
};

const FsInfoCard = () => (
  <DashboardCard
    avatarIcon="eos-icons:file-system"
    stats={<FsStats />}
    title="FileSystems"
  />
);

export default FsInfoCard;
