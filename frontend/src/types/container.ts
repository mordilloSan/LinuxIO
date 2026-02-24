export interface ContainerPort {
  IP?: string;
  PrivatePort: number;
  PublicPort?: number;
  Type: string;
}

export interface ContainerMount {
  Type: string;
  Source: string;
  Destination: string;
  Mode: string;
  RW: boolean;
}

export interface ContainerEndpoint {
  IPAddress: string;
  Gateway: string;
  MacAddress?: string;
  GlobalIPv6Address?: string;
}

export interface ContainerInfo {
  Id: string;
  Names: string[];
  Image: string;
  Created: number;
  State: string;
  Status: string;
  Ports?: ContainerPort[];
  Labels?: Record<string, string>;
  Mounts?: ContainerMount[];
  NetworkSettings?: {
    Networks?: Record<string, ContainerEndpoint>;
  };
  HostConfig?: {
    NetworkMode?: string;
  };
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
