import { FC } from "react";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

const Page: FC = () => {
  const theme = useAppTheme();
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
        <AppTypography variant="h4" component="h1">
          Interface Dashboard
        </AppTypography>
        <CreateInterfaceButton />
      </div>
      <WireGuardDashboard />
    </>
  );
};

export default Page;
