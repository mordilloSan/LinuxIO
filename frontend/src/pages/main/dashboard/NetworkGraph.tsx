import React from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";

interface NetworkGraphProps {
  data: { time: number; rx: number; tx: number }[];
}

// Custom Legend component
const CustomLegend: React.FC<{ latestData?: { rx: number; tx: number } }> = ({
  latestData,
}) => {
  if (!latestData) return null;

  return (
    <div
      style={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        gap: 12,
        marginTop: 4,
        fontSize: 12,
        whiteSpace: "nowrap",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
        <div
          style={{
            width: 8,
            height: 8,
            backgroundColor: "#8884d8",
            borderRadius: "50%",
          }}
        />
        Rx: {latestData.rx.toFixed(2)} kB/s
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
        <div
          style={{
            width: 8,
            height: 8,
            backgroundColor: "#82ca9d",
            borderRadius: "50%",
          }}
        />
        Tx: {latestData.tx.toFixed(2)} kB/s
      </div>
    </div>
  );
};

const NetworkGraph: React.FC<NetworkGraphProps> = ({ data }) => {
  const latest = data.length > 0 ? data[data.length - 1] : undefined;

  return (
    <ResponsiveContainer width="100%" height={120} minWidth={0}>
      <LineChart data={data} margin={{ top: 0, right: 10, left: 0, bottom: 0 }}>
        <XAxis dataKey="time" hide />
        <YAxis hide />
        <Tooltip
          cursor={false}
          formatter={(value) =>
            value != null ? `${Number(value).toFixed(2)} kB/s` : ""
          }
          labelFormatter={() => ""}
          contentStyle={{
            backgroundColor: "rgba(0, 0, 0, 0.7)",
            border: "none",
            borderRadius: 4,
            padding: "2px 6px",
            fontSize: 11,
            color: "#fff",
            lineHeight: 1.2,
            boxShadow: "0 0 5px rgba(0,0,0,0.3)",
          }}
          wrapperStyle={{
            pointerEvents: "none",
          }}
          position={{ y: 10 }}
          isAnimationActive={false}
        />

        <Line
          type="monotone"
          dataKey="rx"
          stroke="#8884d8"
          dot={false}
          name="Rx"
          strokeWidth={2}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="tx"
          stroke="#82ca9d"
          dot={false}
          name="Tx"
          strokeWidth={2}
          isAnimationActive={false}
        />
        <Legend
          verticalAlign="bottom"
          height={0}
          content={<CustomLegend latestData={latest} />}
        />
      </LineChart>
    </ResponsiveContainer>
  );
};

export default NetworkGraph;
