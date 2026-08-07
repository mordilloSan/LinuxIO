import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

const WireguardPage = () => {
  const theme = useAppTheme();
  const { data: interfaces } = useSuspenseQuery(
    linuxio.wireguard.list_interfaces.queryOptions({
      refetchInterval: 10000,
    }),
  );

  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: theme.spacing(2),
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
