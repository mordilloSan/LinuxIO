import type { DiskPowerData } from "@/api";

export interface SmartAttribute {
  id: number;
  name: string;
  raw?: { string?: string; value?: unknown };
  thresh: number;
  value: number;
  worst: number;
}

export interface SmartData extends Record<string, unknown> {
  ata_smart_attributes?: { table?: SmartAttribute[] };
  ata_smart_self_test_log?: { standard?: { table?: unknown[] } };
  device?: Record<string, unknown>;
  firmware_version?: unknown;
  model_name?: unknown;
  nvme_number_of_namespaces?: unknown;
  nvme_self_test_log?: { table?: unknown[] };
  nvme_smart_health_information_log?: Record<string, unknown>;
  nvme_version?: unknown;
  power_cycle_count?: unknown;
  power_on_time?: { hours?: unknown };
  smart_status?: { passed?: boolean };
  temperature?: { current?: unknown };
}

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

export interface SmartTestResult {
  device?: string;
  duration_ms?: number;
  message?: string;
  status?: string;
  test_type?: "short" | "long";
}
