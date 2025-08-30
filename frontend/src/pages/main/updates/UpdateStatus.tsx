import { Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useMemo } from "react";

import UpdateActions from "./UpdateActions";
import UpdateList from "./UpdateList";

import { usePackageUpdater } from "@/hooks/usePackageUpdater";
import { Update } from "@/types/update";
import axios from "@/utils/axios";

const UpdateStatus: React.FC = () => {
  const {
    data,
    isLoading,
    refetch: refetchUpdates,
  } = useQuery<{ updates: Update[] }>({
    queryKey: ["updateInfo"],
    queryFn: () => axios.get("/updates/packages").then((res) => res.data),
    enabled: true,
    refetchInterval: 50000,
  });

  const updates = useMemo(() => data?.updates || [], [data]);

  const { updateOne, updateAll, updatingPackage, progress } =
    usePackageUpdater(refetchUpdates);

  return (
    <Box>
      <UpdateActions
        onUpdateAll={() => updateAll(updates.map((u) => u.package_id))}
        isUpdating={!!updatingPackage}
        currentPackage={updatingPackage}
        progress={progress}
      />

      <UpdateList
        updates={updates}
        onUpdateClick={updateOne}
        isUpdating={!!updatingPackage || isLoading}
        currentPackage={updatingPackage}
        onComplete={refetchUpdates}
        isLoading={isLoading}
      />
    </Box>
  );
};

export default UpdateStatus;
