// src/types/fs.ts
export interface FilesystemInfo {
  device: string;
  fstype: string;
  mountpoint: string;
  total: number;
  used: number;
  free: number;
  usedPercent: number;
}
