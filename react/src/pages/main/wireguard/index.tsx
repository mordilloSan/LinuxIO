import { Typography, Box } from "@mui/material";
import { FC } from "react";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

const Page: FC = () => {
  return (
    <>
      <Box
        display="flex"
        alignItems="center"
        justifyContent="space-between"
        mb={2}
      >
        <Typography variant="h4" component="h1">
          Interface Dashboard
        </Typography>
        <CreateInterfaceButton />
      </Box>
      <WireGuardDashboard />
    </>
  );
};

export default Page;
