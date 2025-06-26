import { Typography, Box } from "@mui/material";
import WireGuardDashboard from "./WireguardDashboard";
import CreateInterfaceButton from "./CreateInterfaceButton"

const Page = () => {

  return (
    <>
      <Box display="flex" alignItems="center" justifyContent="space-between" mb={2}>
        <Typography variant="h4" component="h1">
          Interface Dashboard
        </Typography>
        <CreateInterfaceButton/>
      </Box>
      <WireGuardDashboard/>
    </>
  );
};

export default Page;
