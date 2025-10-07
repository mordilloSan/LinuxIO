export interface ContainerInfo {
  Id: string;
  Names: string[];
  State: string;
  Status: string;
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
