export interface SmartAttribute {
  id: number;
  name: string;
  value: number;
  worst: number;
  thresh: number;
  raw: { value: number; string?: string };
}

export interface SmartData {
  smart_status?: { passed?: boolean };
  temperature?: { current?: number };
  power_on_time?: { hours?: number };
  power_cycle_count?: number;
  ata_smart_attributes?: { table?: SmartAttribute[] };
  nvme_smart_health_information_log?: {
    temperature?: number;
    power_on_hours?: number;
    power_cycles?: number;
    percentage_used?: number;
    data_units_read?: number;
    data_units_written?: number;
  };
}

export interface PowerState {
  state: number;
  maxPowerW: number;
  description: string;
}

export interface PowerData {
  currentState: number;
  estimatedW: number;
  states: PowerState[];
}

export interface DriveInfo {
  name: string;
  model: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
  serial?: string;
  ro?: boolean;
  smart?: SmartData;
  power?: PowerData;
}

export interface SmartTestProgressEvent {
  type: "status" | "progress";
  device?: string;
  test_type?: "short" | "long";
  status?:
    | "starting"
    | "running"
    | "completed"
    | "aborted"
    | "failed"
    | "error"
    | "unknown";
  message?: string;
  percentage?: number;
  remaining_percent?: number;
  remaining_minutes?: number;
}

export interface SmartTestResult {
  device?: string;
  test_type?: "short" | "long";
  status?: string;
  message?: string;
  duration_ms?: number;
}
