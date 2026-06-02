export interface SmartAttribute {
  id: number;
  name: string;
  raw: { value: number; string?: string };
  thresh: number;
  value: number;
  worst: number;
}

export interface SmartData {
  ata_smart_attributes?: { table?: SmartAttribute[] };
  nvme_smart_health_information_log?: {
    temperature?: number;
    power_on_hours?: number;
    power_cycles?: number;
    percentage_used?: number;
    data_units_read?: number;
    data_units_written?: number;
  };
  power_cycle_count?: number;
  power_on_time?: { hours?: number };
  smart_status?: { passed?: boolean };
  temperature?: { current?: number };
}

export interface PowerState {
  description: string;
  maxPowerW: number;
  state: number;
}

export interface PowerData {
  currentState: number;
  estimatedW: number;
  states: PowerState[];
}

export interface DriveInfo {
  model: string;
  name: string;
  power?: PowerData;
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
