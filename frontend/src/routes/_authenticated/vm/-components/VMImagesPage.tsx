import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";

import { VMImagesTab } from "./VMTabs";

const VMImagesPage = () => {
  // Observes preflight only, so the 5s VM-list poll does not re-render it.
  const { data: preflight } = useSuspenseQuery(
    linuxio.virt.preflight.queryOptions(
      {},
      {
        refetchOnMount: false,
      },
    ),
  );

  return <VMImagesTab preflight={preflight} />;
};

export default VMImagesPage;
