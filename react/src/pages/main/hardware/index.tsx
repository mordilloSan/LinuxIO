// ContainerList.tsx
import React, { useState } from "react";

const ContainerList: React.FC = () => {
  const [containers, setContainers] = useState<any[]>([]);

  return (
    <div>
      <h2>Docker Containers </h2>
      <pre style={{ fontSize: 12, maxHeight: 400, overflow: "auto" }}>
        {JSON.stringify(containers, null, 2)}
      </pre>
    </div>
  );
};

export default ContainerList;
