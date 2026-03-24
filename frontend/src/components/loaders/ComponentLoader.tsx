import React from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";

function ComponentLoader() {
  return (
    <div
      style={{
        justifyContent: "center",
        alignItems: "center",
        display: "flex",
        minHeight: "100%",
      }}
    >
      <AppCircularProgress />
    </div>
  );
}

export default ComponentLoader;
