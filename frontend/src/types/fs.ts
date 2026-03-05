// src/types/fs.ts
export interface FilesystemInfo {
  device: string;
  fstype: string;
  mountpoint: string;
  readOnly?: boolean;
  total: number;
  used: number;
  free: number;
  usedPercent: number;
  inodesTotal?: number;
  inodesUsed?: number;
  inodesFree?: number;
  inodesUsedPercent?: number;
}

export interface ResourceStatData {
  mode: string;
  owner: string;
  group: string;
  size: number;
  modified: string;
  raw: string;
  permissions: string;
  path: string;
  realPath: string;
  name: string;
}
