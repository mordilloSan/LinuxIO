"use client";

import React, { useState, useEffect, useRef, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Box } from "@mui/material";
import DataTable from "@/components/tables/GenericTable";
import isEqualWith from "lodash/isEqualWith";
import { useAuthenticatedFetch } from "@/utils/customFetch";
import RouterLink from "@/components/RouterLink";

function Services() {
  const customFetch = useAuthenticatedFetch();
  const { data: ServiceInfo } = useQuery({
    queryKey: ["serviceInfo"],
    queryFn: () => customFetch("/api/system-status/services"),
    refetchInterval: 50000,
  });

  const [rows, setRows] = useState([]);
  const prevDataRef = useRef();

  const columns = useMemo(
    () => [
      {
        id: "name",
        label: "Name",
        accessor: "name",
        width: "20%",
        Cell: ({ value }) => (
          <RouterLink href={`/services/${value}`}>{value}</RouterLink>
        ), // Now using value from the cell as per GenericTable
      },
      {
        id: "loadState",
        label: "Load State",
        accessor: "loadState",
        width: "10%",
      },
      {
        id: "activeState",
        label: "Active State",
        accessor: "activeState",
        width: "10%",
      },
      {
        id: "description",
        label: "Description",
        accessor: "description",
        width: "60%",
      },
    ],
    []
  );

  useEffect(() => {
    if (ServiceInfo && Array.isArray(ServiceInfo.services)) {
      const services = ServiceInfo.services.map((service) => {
        const { name, loadState, activeState, description } = service;
        return {
          name,
          loadState,
          activeState,
          description,
        };
      });

      if (!isEqualWith(prevDataRef.current, services)) {
        prevDataRef.current = services;
        setRows(services);
      }
    }
  }, [ServiceInfo, columns]);

  return (
    <Box>
      <DataTable columns={columns} rows={rows} title="Services List" />
    </Box>
  );
}

export default Services;
