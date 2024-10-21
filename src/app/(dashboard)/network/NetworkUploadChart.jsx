"use client";

import React, { useEffect } from "react";
import { Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import ChartComponent from "@/components/charts/ReactApexChart";
import { graphRange } from "@/configs/cardConfig";
import { formatDataRate } from "@/utils/formatter";
import ApexCharts from "apexcharts";
import { useAuthenticatedFetch } from "@/utils/customFetch";

// Vars
const divider = "var(--mui-palette-divider)";
const disabledText = "var(--mui-palette-text-disabled)";

const chartOptions = {
  chart: {
    id: "realtime2",
    animations: {
      enabled: true,
      easing: "linear",
      dynamicAnimation: {
        speed: 1500,
      },
    },
    toolbar: {
      show: false,
    },
    zoom: {
      enabled: false,
    },
  },
  grid: {
    padding: { top: -10 },
    borderColor: divider,
  },
  stroke: {
    curve: "smooth",
    width: 2.5,
  },
  markers: {
    size: 0,
  },
  colors: ["#3CB371"],
  xaxis: {
    axisBorder: { show: false },
    axisTicks: { color: divider },
    crosshairs: {
      stroke: { color: divider },
    },
    type: "datetime",
    range: graphRange,
    labels: {
      show: false,
      style: { colors: disabledText, fontSize: "12px" },
      format: "HH:mm:ss",
    },
  },
  yaxis: {
    forceNiceScale: true,
    labels: {
      show: true,
      style: { colors: disabledText, fontSize: "12px" },
      formatter: (val) => formatDataRate(val)[0],
    },
  },
  annotations: {
    yaxis: [
      {
        y: 0, // y value for the horizontal line
      },
    ],
  },
  tooltip: {
    enabled: false,
    y: {
      formatter: (val) => {
        const [formattedValue, unit] = formatDataRate(val);
        return `${formattedValue} ${unit}`;
      },
    },
    x: { show: false },
  },
  legend: {
    show: true,
    position: "bottom",
    offsetY: 5,
    itemMargin: {
      horizontal: 2,
      vertical: 0,
    },
    labels: {
      colors: disabledText,
    },
    formatter: (seriesName, opts) => {
      const seriesData = opts.w.config.series[opts.seriesIndex].data;
      const lastDataPoint = seriesData[seriesData.length - 1] || { y: 0 };
      const [formattedValue, unit] = formatDataRate(lastDataPoint.y);
      return `${seriesName}: ${Math.abs(formattedValue)} ${unit}`;
    },
  },
  series: [
    {
      name: "Upload",
      data: [],
    },
  ],
};

const NetworkUploadChart = () => {
  const customFetch = useAuthenticatedFetch();
  const { data, error, isLoading } = useQuery({
    queryKey: ["networkInfo"],
    queryFn: () => customFetch("/api/network/networkinfo"),
    refetchInterval: 1000,
  });

  useEffect(() => {
    if (data && !isLoading && !error) {
      const serverTimestamp = new Date(data.timestamp).getTime(); // Use timestamp from the API response
      ApexCharts.exec("realtime2", "appendData", [
        {
          name: "Upload",
          data: [{ x: serverTimestamp, y: data.totalTxSec }],
        },
      ]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  return (
    <Box
      sx={{
        mt: {
          xs: 0,
          sm: 0,
          xl: -5,
        },
        width: {
          xs: "100%", // 100% width on extra-small screens
          sm: "100%", // 400px width on small screens
          xl: "100%",
        },
        minWidth: {
          xl: 190,
          sm: 250,
          xs: 400,
        },
      }}
    >
      <ChartComponent options={chartOptions} series={chartOptions.series} />
    </Box>
  );
};

export default NetworkUploadChart;
