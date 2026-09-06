import { describe, expect, it, vi } from "vitest";

import type {
  LiveInterface,
  MonitoringNetworkHistoryPoint,
  NetworkInterface,
} from "@/api";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen } from "@/test/render";

import NetworkInterfaceLogsCard from "./NetworkInterfaceLogsCard";
import NetworkInterfaceStatsCard, {
  networkInterfaceStatRows,
} from "./NetworkInterfaceStatsCard";
import { networkHistorySeries } from "./NetworkTrafficHistoryCard";

const mocks = vi.hoisted(() => ({
  interfaces: [] as NetworkInterface[],
  live: { interfaces: {} },
}));

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

// The interface list owns the poll; the cards only select out of its cache, so
// the mock hands each card whatever its own selector pulls from the fixture.
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: ({
      queryKey,
      select,
    }: {
      queryKey?: readonly unknown[];
      select?: (data: unknown) => unknown;
    }) => {
      const data = queryKey?.includes("get_live")
        ? mocks.live
        : mocks.interfaces;
      return {
        data: select ? select(data) : data,
        error: null,
        isLoading: false,
      };
    },
  };
});

vi.mock("@/components/cards/UnitLogsCard", () => ({
  default: ({ title, unitName }: { title: string; unitName?: string }) => (
    <div data-testid="unit-logs" data-unit={unitName}>
      {title}
    </div>
  ),
}));

type StatRows = ReturnType<typeof networkInterfaceStatRows>;

const rowValue = (rows: StatRows, label: string) =>
  rows.find((row) => row.label === label)?.value;

const withInterfaces = (...interfaces: NetworkInterface[]) => {
  mocks.interfaces = interfaces;
};

const withLiveInterface = (name: string, live: LiveInterface) => {
  mocks.live = { interfaces: { [name]: live } };
};

it("keeps throughput on interface cards using the live monitoring sample", () => {
  withInterfaces(testNetworkInterface());
  withLiveInterface("eth0", {
    rx_bytes_per_sec: 2048,
    tx_bytes_per_sec: 1024,
    rx_bytes_total: 0,
    tx_bytes_total: 0,
    rx_packets: 0,
    tx_packets: 0,
    rx_errors: 0,
    tx_errors: 0,
    rx_dropped: 0,
    tx_dropped: 0,
  });
  render(
    <NetworkInterfaceCard name="eth0" type="ethernet" onToggle={() => {}} />,
  );
  expect(
    screen.getByText("RX/s: 2.0 kB/s | TX/s: 1.0 kB/s"),
  ).toBeInTheDocument();
});

describe("networkInterfaceStatRows", () => {
  it("reports the link, the driver and the managing backend", () => {
    const rows = networkInterfaceStatRows(
      testNetworkInterface({
        config_backend: "systemd-networkd",
        driver: "r8169",
        duplex: "full",
        mtu: 9000,
        speed: "1000 Mbps",
      }),
    );

    expect(rowValue(rows, "Link")).toBe("1000 Mbps (full)");
    expect(rowValue(rows, "MTU")).toBe("9000");
    expect(rowValue(rows, "Driver")).toBe("r8169");
    expect(rowValue(rows, "Managed by")).toBe("systemd-networkd");
  });

  it("names the gaps instead of implying a value", () => {
    const rows = networkInterfaceStatRows(
      testNetworkInterface({
        carrier: undefined,
        config_backend: "",
        driver: "",
        speed: "unknown",
      }),
    );

    expect(rowValue(rows, "Link")).toBe("No carrier");
    expect(rowValue(rows, "Driver")).toBe("—");
    expect(rowValue(rows, "Managed by")).toBe("Unmanaged");
  });

  it("leaves carrier and operstate to the status dot", () => {
    const labels = networkInterfaceStatRows(testNetworkInterface()).map(
      (row) => row.label,
    );

    expect(labels).not.toContain("Carrier");
    expect(labels).not.toContain("State");
  });

  it("flags non-zero error and drop counters", () => {
    const clean = networkInterfaceStatRows(testNetworkInterface());
    expect(clean.find((row) => row.label === "Errors (tx/rx)")?.warn).toBe(
      false,
    );

    const rows = networkInterfaceStatRows(testNetworkInterface(), {
      rx_bytes_per_sec: 0,
      tx_bytes_per_sec: 0,
      rx_bytes_total: 2048,
      tx_bytes_total: 1024,
      rx_dropped: 0,
      rx_errors: 4,
      rx_packets: 12,
      tx_dropped: 7,
      tx_errors: 0,
      tx_packets: 6,
    });

    expect(rowValue(rows, "Sent")).toBe("1 KB · 6 pkt");
    expect(rowValue(rows, "Received")).toBe("2 KB · 12 pkt");
    expect(rowValue(rows, "Errors (tx/rx)")).toBe("0 / 4");
    expect(rows.find((row) => row.label === "Errors (tx/rx)")?.warn).toBe(true);
    expect(rows.find((row) => row.label === "Dropped (tx/rx)")?.warn).toBe(
      true,
    );
  });

  it("uses zeroes while the live sample is unavailable", () => {
    const rows = networkInterfaceStatRows(testNetworkInterface());

    expect(rowValue(rows, "Sent")).toBe("0 Bytes · 0 pkt");
    expect(rowValue(rows, "Received")).toBe("0 Bytes · 0 pkt");
    expect(rowValue(rows, "Errors (tx/rx)")).toBe("0 / 0");
    expect(rowValue(rows, "Dropped (tx/rx)")).toBe("0 / 0");
  });
});

describe("NetworkInterfaceStatsCard", () => {
  it("renders the selected interface's counters", () => {
    withInterfaces(
      testNetworkInterface({ mtu: 1400, name: "eth1" }),
      testNetworkInterface({ mtu: 1500, name: "eth0" }),
    );

    withLiveInterface("eth1", {
      rx_bytes_per_sec: 0,
      tx_bytes_per_sec: 0,
      rx_bytes_total: 2048,
      tx_bytes_total: 1024,
      rx_dropped: 0,
      rx_errors: 0,
      rx_packets: 12,
      tx_dropped: 0,
      tx_errors: 0,
      tx_packets: 6,
    });

    render(<NetworkInterfaceStatsCard name="eth1" />);

    expect(screen.getByText("1400")).toBeTruthy();
    expect(screen.getByText("1 KB · 6 pkt")).toBeTruthy();
  });

  it("renders nothing while the interface is absent from the cache", () => {
    withInterfaces(testNetworkInterface({ name: "eth0" }));

    const { container } = render(<NetworkInterfaceStatsCard name="wlan0" />);

    expect(container.firstChild).toBeNull();
  });
});

describe("NetworkInterfaceLogsCard", () => {
  it("tails the unit the bridge resolved for the interface", () => {
    withInterfaces(
      testNetworkInterface({
        log_unit: "systemd-networkd.service",
        name: "eth0",
      }),
    );

    render(<NetworkInterfaceLogsCard name="eth0" />);

    expect(screen.getByTestId("unit-logs").dataset.unit).toBe(
      "systemd-networkd.service",
    );
  });

  it("offers no log view when no candidate unit is installed", () => {
    withInterfaces(testNetworkInterface({ log_unit: "", name: "eth0" }));

    const { container } = render(<NetworkInterfaceLogsCard name="eth0" />);

    expect(container.firstChild).toBeNull();
  });
});

describe("networkHistorySeries", () => {
  const point = (
    t: number,
    interfaces?: MonitoringNetworkHistoryPoint["interfaces"],
  ): MonitoringNetworkHistoryPoint => ({
    captured_at_ms: t,
    interfaces,
    recv_bytes_per_sec: 0,
    sent_bytes_per_sec: 0,
  });

  it("keeps only the samples that carry this interface", () => {
    const series = networkHistorySeries(
      [
        point(1, { eth0: { recv_bytes_per_sec: 20, sent_bytes_per_sec: 10 } }),
        point(2),
        point(3, { eth1: { recv_bytes_per_sec: 90, sent_bytes_per_sec: 80 } }),
        point(4, { eth0: { recv_bytes_per_sec: 40, sent_bytes_per_sec: 30 } }),
      ],
      "eth0",
      { rx: "#rx", tx: "#tx" },
    );

    expect(series.map((entry) => entry.label)).toEqual(["Sent", "Received"]);
    expect(series[0].points).toEqual([
      { t: 1, v: 10 },
      { t: 4, v: 30 },
    ]);
    expect(series[1].points).toEqual([
      { t: 1, v: 20 },
      { t: 4, v: 40 },
    ]);
    expect(series[0].color).toBe("#tx");
    expect(series[1].color).toBe("#rx");
  });

  it("yields empty series before any history arrives", () => {
    const series = networkHistorySeries(undefined, "eth0", {
      rx: "#rx",
      tx: "#tx",
    });

    expect(series.every((entry) => entry.points.length === 0)).toBe(true);
  });
});
