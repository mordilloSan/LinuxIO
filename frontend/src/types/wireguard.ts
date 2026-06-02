// Define the interface type
export interface WireGuardInterface {
  address: string;
  isConnected: "Active" | "Inactive" | string;
  isEnabled: boolean;
  name: string;
  peerCount: number;
  port: number;
}
