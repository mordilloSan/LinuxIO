export interface ContainerPort {
  IP?: string;
  PrivatePort: number;
  PublicPort?: number;
  Type: string;
}

export interface ContainerMount {
  Destination: string;
  Mode: string;
  RW: boolean;
  Source: string;
  Type: string;
}

export interface ContainerEndpoint {
  Gateway: string;
  GlobalIPv6Address?: string;
  IPAddress: string;
  MacAddress?: string;
}

export interface ContainerInfo {
  Created: number;
  HostConfig?: {
    NetworkMode?: string;
  };
  icon?: string;
  Id: string;
  Image: string;
  Labels?: Record<string, string>;
  metrics?: {
    cpu_percent: number;
    mem_usage: number;
    mem_limit: number;
    net_input: number;
    net_output: number;
    block_read: number;
    block_write: number;
  };
  Mounts?: ContainerMount[];
  Names: string[];
  NetworkSettings?: {
    Networks?: Record<string, ContainerEndpoint>;
  };
  Ports?: ContainerPort[];
  proxyPort?: string;
  State: string;
  Status: string;
  url?: string;
}
