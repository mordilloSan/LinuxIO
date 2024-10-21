"use client";

import React, { useEffect } from "react";
import { Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import ApexCharts from "apexcharts";
import ChartComponent from "@/components/charts/ReactApexChart";
import { graphRange } from "@/configs/cardConfig";
import { formatDataRate } from "@/utils/formatter";
import { useAuthenticatedFetch } from "@/utils/customFetch";

// Vars
const MAX_DATA_POINTS = 30; // Limit the number of data points on the chart
const divider = "var(--mui-palette-divider)";
const disabledText = "var(--mui-palette-text-disabled)";

const chartOptions = {
  chart: {
    id: "realtime", // Unique ID for this chart (used with ApexCharts.exec)
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
  colors: ["#1E90FF"], // Use a blue color for the "Download" data
  xaxis: {
    axisBorder: { show: false },
    axisTicks: { color: divider },
    crosshairs: {
      stroke: { color: divider },
    },
    type: "datetime",
    range: graphRange, // You can adjust this to control the range of the x-axis
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
      formatter: (val) => formatDataRate(val)[0], // Custom data formatter for download speed
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
      name: "Down", // The name of the series
      data: [], // Initial empty data array
    },
  ],
};

const NetworkDownloadChart = () => {
  const customFetch = useAuthenticatedFetch();

  const { data, error, isLoading } = useQuery({
    queryKey: ["networkInfo"],
    queryFn: () => customFetch("/api/network/networkinfo"),
    refetchInterval: 1000, // Fetch new data every second for real-time updates
  });

  useEffect(() => {
    if (data && !isLoading && !error) {
      const serverTimestamp = new Date(data.timestamp).getTime(); // Get the timestamp from the API response

      // Append the new download data point to the chart
      ApexCharts.exec("realtime", "appendData", [
        {
          name: "Down",
          data: [{ x: serverTimestamp, y: data.totalRxSec }],
        },
      ]);

      // Limit the number of data points and adjust x-axis range dynamically
      ApexCharts.exec("realtime", "updateOptions", {
        xaxis: {
          min: serverTimestamp - graphRange,
          max: serverTimestamp,
        },
      });
    }
  }, [data, isLoading, error]);

  if (isLoading) return <p>Loading...</p>;
  if (error) return <p>Error loading data</p>;

  return (
    <Box
      sx={{
        mt: { xs: 0, sm: 0, xl: -5 },
        width: { xs: "100%", sm: "100%", xl: "100%" },
        minWidth: { xl: 190, sm: 250, xs: 400 },
      }}
    >
      <ChartComponent options={chartOptions} series={chartOptions.series} />
    </Box>
  );
};

export default NetworkDownloadChart;
