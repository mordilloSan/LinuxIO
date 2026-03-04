import { Stack, Typography } from "@mui/material";
import { FC } from "react";

import CreateInterfaceButton from "./CreateInterfaceButton";
import WireGuardDashboard from "./WireguardDashboard";

const Page: FC = () => {
  return (
    <>
      <Stack
        direction="row"
        alignItems="center"
        justifyContent="space-between"
        sx={{ mb: 2 }}
      >
        <Typography variant="h4" component="h1">
          Interface Dashboard
        </Typography>
        <CreateInterfaceButton />
      </Stack>
      <WireGuardDashboard />
    </>
  );
};

export default Page;
