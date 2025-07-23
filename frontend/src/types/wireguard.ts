// Define the interface type
export type WireGuardInterface = {
  name: string;
  isConnected: "Active" | "Inactive" | string;
  address: string;
  port: number;
  peerCount: number;
};
