import type { VMPreflight } from "@/api";
import AppChip from "@/components/ui/AppChip";
import { useAppTheme } from "@/theme";

export default function PreflightSummary({
  preflight,
  showDefaultNetwork = true,
}: {
  preflight?: VMPreflight;
  showDefaultNetwork?: boolean;
}) {
  const theme = useAppTheme();
  const checks = [
    ["KVM", preflight?.kvmPresent],
    ["QEMU", preflight?.qemuPresent],
    ["libvirt", preflight?.libvirtReachable],
    ["default pool", preflight?.defaultPoolExists],
    ...(showDefaultNetwork
      ? ([["default network", preflight?.defaultNetworkExists]] as const)
      : []),
    [
      "firmware",
      preflight?.firmware.biosAvailable || preflight?.firmware.uefiAvailable,
    ],
  ] as const;

  return (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        flexWrap: "wrap",
        gap: theme.spacing(2),
      }}
    >
      {checks.map(([label, ok]) => (
        <AppChip
          color={ok ? "success" : ok === false ? "warning" : "default"}
          key={label}
          label={label}
          size="small"
          variant="soft"
        />
      ))}
    </div>
  );
}
