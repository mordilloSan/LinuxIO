import React, { useState } from "react";

import ModuleCard from "./ModuleCard";
import ModuleDetailsDrawer from "./ModuleDetailsDrawer";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppGrid from "@/components/ui/AppGrid";

const ModulesList: React.FC = () => {
  const [selectedModule, setSelectedModule] = useState<string | null>(null);
  const [detailsDrawerOpen, setDetailsDrawerOpen] = useState(false);

  const {
    data: modules,
    isPending,
    isError,
    error,
    refetch,
  } = linuxio.modules.get_modules.useQuery({
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
      <AppAlert severity="error">
        {error instanceof Error ? error.message : "Failed to load modules"}
      </AppAlert>
    );
  }

  if (!modules || modules.length === 0) {
    return (
      <AppAlert severity="info" style={{ marginTop: 16 }}>
        No modules installed. Go to the Install tab to add modules.
      </AppAlert>
    );
  }

  return (
    <div style={{ marginTop: 8 }}>
      <AppGrid container spacing={2}>
        {modules.map((module) => (
          <AppGrid key={module.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <ModuleCard
              module={module}
              onViewDetails={() => handleViewDetails(module.name)}
              onModuleChange={handleModuleChange}
            />
          </AppGrid>
        ))}
      </AppGrid>

      <ModuleDetailsDrawer
        open={detailsDrawerOpen}
        onClose={() => setDetailsDrawerOpen(false)}
        moduleName={selectedModule}
      />
    </div>
  );
};

export default ModulesList;
