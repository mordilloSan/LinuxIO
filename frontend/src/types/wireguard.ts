// Define the interface type
export interface WireGuardInterface {
  name: string;
  isConnected: "Active" | "Inactive" | string;
  address: string;
  port: number;
  peerCount: number;
  isEnabled: boolean;
}
