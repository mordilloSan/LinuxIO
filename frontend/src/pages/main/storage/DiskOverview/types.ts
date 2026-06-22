import type { DiskPowerData, SmartData } from "@/api";

export type { SmartAttribute, SmartData } from "@/api";

export type SmartDeviceInfo = NonNullable<SmartData["device"]>;
export type SmartStatus = NonNullable<SmartData["smart_status"]>;
export type SmartNVMeHealthInformationLog = NonNullable<
  SmartData["nvme_smart_health_information_log"]
>;
export type SmartATASelfTestLog = NonNullable<
  SmartData["ata_smart_self_test_log"]
>;
export type SmartNVMeSelfTestLog = NonNullable<SmartData["nvme_self_test_log"]>;
export type SmartStandardSelfTestRow = NonNullable<
  NonNullable<SmartATASelfTestLog["standard"]>["table"]
>[number];
export type SmartNVMeSelfTestRow = NonNullable<
  SmartNVMeSelfTestLog["table"]
>[number];

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
