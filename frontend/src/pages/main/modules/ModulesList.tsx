import { Box, Grid, Alert } from "@mui/material";
import React, { useState } from "react";

import ModuleCard from "./ModuleCard";
import ModuleDetailsDrawer from "./ModuleDetailsDrawer";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import type { ModuleInfo } from "@/types/module";

const ModulesList: React.FC = () => {
  const [selectedModule, setSelectedModule] = useState<string | null>(null);
  const [detailsDrawerOpen, setDetailsDrawerOpen] = useState(false);

  const {
    data: modules,
    isPending,
    isError,
    error,
    refetch,
  } = linuxio.useCall<ModuleInfo[]>("modules", "GetModules", [], {
    refetchInterval: 5000,
  });

  const handleViewDetails = (moduleName: string) => {
    setSelectedModule(moduleName);
    setDetailsDrawerOpen(true);
  };

  const handleModuleChange = () => {
    refetch();
  };

  if (isPending) return <ComponentLoader />;

  if (isError) {
    return (
      <Alert severity="error">
        {error instanceof Error ? error.message : "Failed to load modules"}
      </Alert>
    );
  }

  if (!modules || modules.length === 0) {
    return (
      <Alert severity="info" sx={{ mt: 2 }}>
        No modules installed. Go to the Install tab to add modules.
      </Alert>
    );
  }

  return (
    <Box sx={{ mt: 2 }}>
      <Grid container spacing={2}>
        {modules.map((module) => (
          <ModuleCard
            key={module.name}
            module={module}
            onViewDetails={() => handleViewDetails(module.name)}
            onModuleChange={handleModuleChange}
          />
        ))}
      </Grid>

      <ModuleDetailsDrawer
        open={detailsDrawerOpen}
        onClose={() => setDetailsDrawerOpen(false)}
        moduleName={selectedModule}
      />
    </Box>
  );
};

export default ModulesList;
