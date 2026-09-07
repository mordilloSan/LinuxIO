import type { DiskPowerData, SmartData } from "@/api";

export type { SmartAttribute, SmartData } from "@/api";

export interface DriveInfo {
  model: string;
  name: string;
  power?: DiskPowerData;
  ro?: boolean;
  serial?: string;
  sizeBytes: number;
  smart?: SmartData;
  transport: string;
  vendor?: string;
}

export interface SmartTestProgressEvent {
  device?: string;
  message?: string;
  percentage?: number;
  remaining_minutes?: number;
  remaining_percent?: number;
  status?:
    | "starting"
    | "running"
    | "in_progress"
    | "completed"
    | "aborted"
    | "failed"
    | "error"
    | "unknown";
  test_type?: "short" | "long";
  type: "status" | "progress";
}
