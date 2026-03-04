import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FC } from "react";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

const Page: FC = () => {
  const theme = useTheme();
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
        <Typography variant="h4" component="h1">
          Interface Dashboard
        </Typography>
        <CreateInterfaceButton />
      </div>
      <WireGuardDashboard />
    </>
  );
};

export default Page;
