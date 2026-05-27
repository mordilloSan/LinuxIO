import { FC } from "react";

import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

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
        <AppTypography component="h1" variant="h4">
          Interface Dashboard
        </AppTypography>
        <CreateInterfaceButton />
      </div>
      <WireGuardDashboard />
    </>
  );
};

export default Page;
