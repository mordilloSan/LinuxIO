export interface ContainerInfo {
  Id: string;
  Names: string[];
  Image: string;
  Created: number;
  State: string;
  Status: string;
  icon?: string;
  url?: string;
  metrics?: {
    cpu_percent: number;
    mem_usage: number;
    mem_limit: number;
    net_input: number;
    net_output: number;
    block_read: number;
    block_write: number;
  };
}
