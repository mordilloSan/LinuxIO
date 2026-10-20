import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import AppTypography from "@/components/ui/AppTypography";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

const WireguardPage = () => {
  const { data: interfaces } = useSuspenseQuery({
    ...linuxio.wireguard.list_interfaces,
    refetchInterval: 10000,
  });

  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          marginBottom: "var(--app-space-8)",
        }}
      >
        <AppTypography component="h1" variant="h4">
          Interface Dashboard
        </AppTypography>
        <CreateInterfaceButton interfaces={interfaces} />
      </div>
      <WireGuardDashboard interfaces={interfaces} />
    </>
  );
};

export default WireguardPage;
