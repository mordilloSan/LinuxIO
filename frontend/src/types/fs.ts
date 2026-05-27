// src/types/fs.ts
export interface FilesystemInfo {
  device: string;
  free: number;
  fstype: string;
  inodesFree?: number;
  inodesTotal?: number;
  inodesUsed?: number;
  inodesUsedPercent?: number;
  mountpoint: string;
  readOnly?: boolean;
  total: number;
  used: number;
  usedPercent: number;
}

export interface ResourceStatData {
  group: string;
  mode: string;
  modified: string;
  name: string;
  owner: string;
  path: string;
  permissions: string;
  raw: string;
  realPath: string;
  size: number;
}
