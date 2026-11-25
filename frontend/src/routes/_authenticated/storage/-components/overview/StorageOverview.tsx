import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";

import { linuxio, type BtrfsFilesystem } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import { STATUS_COLOR } from "@/components/cards/EntityCardKit";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";

import { poolHealth } from "../btrfs/BtrfsHealthBanner";
import { disksItSurvives } from "../btrfs/btrfsShared";
import { describePool } from "../disk-overview/storageGraph";
import CreatePoolButton from "../pools/CreatePoolButton";
import { POOLS_ACCESS } from "../pools/poolsAccess";
import PoolSummaryCard from "../pools/PoolSummaryCard";

type Tone = "error" | "warning";

/**
 * One thing worth a look and the page where it is fixed. A problem is
 * something wrong now; advice is a risk the pool runs by design (one copy,
 * no copy elsewhere), listed but not counted against its health.
 */
type Finding = { key: string; tone: Tone; text: string; problem: boolean } & (
  | { pool: string }
  | { disks: { raid?: string } }
);

const poolFindings = (
  fs: BtrfsFilesystem,
  name: string,
  copied: boolean | undefined,
): Finding[] => {
  const findings: Finding[] = [];
  const health = poolHealth(fs, name);
  if (health.color !== STATUS_COLOR.ok) {
    findings.push({
      key: `${fs.uuid}-health`,
      tone: health.color === STATUS_COLOR.error ? "error" : "warning",
      text: `${name}: ${health.title}`,
      pool: fs.uuid,
      problem: true,
    });
  } else if (disksItSurvives(fs.allocation) === 0) {
    findings.push({
      key: `${fs.uuid}-copies`,
      tone: "warning",
      text:
        fs.devices.length === 1
          ? `${name} has one disk: a disk failure loses it`
          : `${name} keeps one copy: a disk failure loses it`,
      pool: fs.uuid,
      problem: false,
    });
  }
  if (copied === false) {
    findings.push({
      key: `${fs.uuid}-replication`,
      tone: "warning",
      text: `${name} is not copied anywhere else`,
      pool: fs.uuid,
      problem: false,
    });
  }
  return findings;
};

/** A shared path without its trailing slash, "/" kept. */
const normalizePath = (path: string) => path.replace(/(.)\/+$/, "$1");

/**
 * Pairs each shared path with the protocols that share it, as the Shared
 * folders tab groups them: a Samba share without a path is its name.
 */
const sharedFolders = (
  samba: { name: string; properties: Record<string, string> }[],
  nfs: { path: string }[],
) => {
  const byPath = new Map<string, Set<string>>();
  const add = (path: string, protocol: string) => {
    const key = normalizePath(path);
    byPath.set(key, (byPath.get(key) ?? new Set()).add(protocol));
  };
  for (const share of samba) add(share.properties.path ?? share.name, "SMB");
  for (const share of nfs) add(share.path, "NFS");
  return [...byPath.entries()]
    .map(([path, protocols]) => ({ path, protocols: [...protocols].sort() }))
    .sort((a, b) => a.path.localeCompare(b.path));
};

/**
 * Storage → Overview: one health line, what is worth a look and where to fix
 * it, the pools and the shared folders.
 */
const StorageOverview = () => {
  const navigate = useNavigate();
  const access = useAccessContext();
  const poolsAllowed = hasAccessPolicy(POOLS_ACCESS, access);
  // Polled as the Disks page polls them, so a pool or array that changes
  // while the page is open shows.
  const poolsQuery = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: poolsAllowed,
    refetchInterval: 10000,
  });
  const offlineQuery = useQuery({
    ...linuxio.storage.btrfs_pools_offline,
    enabled: poolsAllowed,
    refetchInterval: 10000,
  });
  const { data: jobs } = useQuery({
    ...linuxio.storage.btrfs_replication_jobs,
    enabled: poolsAllowed,
  });
  const arraysQuery = useQuery({
    ...linuxio.storage.md_list,
    refetchInterval: 10000,
  });
  const { data: samba = [] } = useQuery(linuxio.shares.list_samba_shares);
  const { data: nfs = [] } = useQuery(linuxio.shares.list_nfs_shares);
  const filesystems = poolsQuery.data ?? [];

  const pools = filesystems.map((fs, index) => ({
    fs,
    name: describePool(fs, index).name,
  }));
  const findings: Finding[] = [
    ...pools.flatMap(({ fs, name }) =>
      poolFindings(
        fs,
        name,
        jobs?.some((job) => job.enabled && job.uuid === fs.uuid),
      ),
    ),
    ...(offlineQuery.data ?? []).map((pool): Finding => ({
      key: `${pool.uuid}-offline`,
      tone: pool.missing > 0 ? "error" : "warning",
      text: `${pool.label || pool.uuid} is not mounted`,
      disks: {},
      problem: true,
    })),
    ...(arraysQuery.data ?? [])
      .filter((array) => array.degraded > 0)
      .map((array): Finding => ({
        key: `${array.name}-degraded`,
        tone: "error",
        text: `${array.name} is running without a disk`,
        disks: { raid: array.name },
        problem: true,
      })),
  ];
  // Problems lead the list, advice follows.
  findings.sort((a, b) => Number(b.problem) - Number(a.problem));
  const problems = findings.filter((finding) => finding.problem).length;
  const checking =
    (poolsAllowed && (poolsQuery.isPending || offlineQuery.isPending)) ||
    arraysQuery.isPending;
  const unread = [
    poolsAllowed && poolsQuery.isError && "Could not read the pools",
    poolsAllowed &&
      offlineQuery.isError &&
      "Could not read the pools that are not mounted",
    arraysQuery.isError && "Could not read the md arrays",
  ].filter((text): text is string => Boolean(text));
  const healthy = !checking && problems === 0 && unread.length === 0;
  const title = checking
    ? "Checking…"
    : problems > 0
      ? problems === 1
        ? "1 problem needs a look"
        : `${problems} problems need a look`
      : unread.length > 0
        ? unread.join("; ")
        : poolsAllowed
          ? "All pools and arrays are healthy"
          : "All arrays are healthy";
  const folders = sharedFolders(samba, nfs);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-16)",
      }}
    >
      <FrostedCard>
        <AppCardContent>
          <CardIconHeader
            headingVariant="section"
            iconTint
            icon={
              <Icon
                height={24}
                icon={
                  healthy
                    ? "mdi:check-circle-outline"
                    : checking
                      ? "mdi:progress-clock"
                      : "mdi:alert-circle-outline"
                }
                width={24}
              />
            }
            subtitle={
              poolsAllowed
                ? "Pools are also checked every ten minutes; their problems go to the bell."
                : undefined
            }
            title={title}
          />
        </AppCardContent>
      </FrostedCard>

      {findings.length > 0 && (
        <FrostedCard aria-label="Worth a look" role="region">
          <AppCardContent>
            <ul
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "var(--app-space-8)",
                listStyle: "none",
                margin: 0,
                padding: 0,
              }}
            >
              {findings.map((finding) => (
                <li
                  key={finding.key}
                  style={{
                    alignItems: "center",
                    display: "flex",
                    gap: "var(--app-space-8)",
                  }}
                >
                  <Icon
                    aria-hidden="true"
                    color={STATUS_COLOR[finding.tone]}
                    height={20}
                    icon={
                      finding.problem ? "mdi:alert-circle" : "mdi:information"
                    }
                    width={20}
                  />
                  {"pool" in finding ? (
                    <Link
                      params={{ uuid: finding.pool }}
                      to="/storage/pools/$uuid"
                    >
                      {finding.text}
                    </Link>
                  ) : (
                    <Link search={finding.disks} to="/storage/disks">
                      {finding.text}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </AppCardContent>
        </FrostedCard>
      )}

      <section aria-label="Pools">
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: "var(--app-space-8)",
            marginBottom: "var(--app-space-8)",
          }}
        >
          <AppTypography component="h2" fontWeight={700} variant="h6">
            Pools
          </AppTypography>
          {poolsAllowed && (
            <div style={{ marginInlineStart: "auto" }}>
              <CreatePoolButton
                pooledDevices={filesystems.flatMap((fs) =>
                  fs.devices.map(({ path }) => path),
                )}
              />
            </div>
          )}
        </div>
        {poolsAllowed ? (
          pools.length > 0 ? (
            <AppGrid container spacing={2}>
              {pools.map(({ fs, name }) => (
                <AppGrid key={fs.uuid} size={{ xs: 12, sm: 6, lg: 4 }}>
                  <PoolSummaryCard
                    fs={fs}
                    name={name}
                    onOpen={() =>
                      void navigate({
                        params: { uuid: fs.uuid },
                        to: "/storage/pools/$uuid",
                      })
                    }
                  />
                </AppGrid>
              ))}
            </AppGrid>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              {poolsQuery.isPending
                ? "Reading the pools…"
                : "No pool is mounted yet."}
            </AppTypography>
          )
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            Pools need the btrfs tools and a session with administrator rights.
          </AppTypography>
        )}
      </section>

      <FrostedCard aria-label="Shared folders" role="region">
        <AppCardContent>
          <CardIconHeader
            iconTint
            icon={
              <Icon height={24} icon="mdi:folder-network-outline" width={24} />
            }
            right={<Link to="/storage/shares">See all</Link>}
            title="Shared folders"
          />
          {folders.length === 0 ? (
            <AppTypography color="text.secondary" variant="body2">
              No folder is shared yet.
            </AppTypography>
          ) : (
            <ul
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "var(--app-space-8)",
                listStyle: "none",
                margin: 0,
                padding: 0,
              }}
            >
              {folders.map(({ path, protocols }) => (
                <li
                  key={path}
                  style={{
                    alignItems: "center",
                    display: "flex",
                    flexWrap: "wrap",
                    gap: "var(--app-space-8)",
                  }}
                >
                  <AppTypography variant="body2">{path}</AppTypography>
                  {protocols.map((protocol) => (
                    <AppChip
                      key={protocol}
                      label={protocol}
                      size="small"
                      variant="soft"
                    />
                  ))}
                </li>
              ))}
            </ul>
          )}
        </AppCardContent>
      </FrostedCard>
    </div>
  );
};

export default StorageOverview;
