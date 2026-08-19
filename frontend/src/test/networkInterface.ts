import type { NetworkInterface } from "@/api";

/**
 * A complete `NetworkInterface` in one place: tests override the two or three
 * fields they care about, so a new field on the wire shape lands here instead
 * of in every test that needs an interface.
 */
export const testNetworkInterface = (
  overrides: Partial<NetworkInterface> = {},
): NetworkInterface => ({
  carrier: true,
  config_backend: "nmconnection",
  counters: {
    rx_bytes: 0,
    rx_dropped: 0,
    rx_errors: 0,
    rx_packets: 0,
    tx_bytes: 0,
    tx_dropped: 0,
    tx_errors: 0,
    tx_packets: 0,
  },
  dns: [],
  driver: "e1000e",
  duplex: "full",
  gateway: "",
  ipv4: [],
  log_unit: "NetworkManager.service",
  mac: "00:11:22:33:44:55",
  mtu: 1500,
  name: "eth0",
  operstate: "up",
  rx_speed: 0,
  speed: "1000",
  state: 100,
  tx_speed: 0,
  type: "ethernet",
  ...overrides,
});
