export interface ModuleInfo {
  name: string;
  title: string;
  description: string;
  version: string;
  route: string;
  icon: string;
  position: number;
  componentUrl: string;
}

export interface SettingConfig {
  name: string;
  type: string;
  default: any;
  description: string;
  min?: any;
  max?: any;
}

export interface ModuleDetailsInfo extends ModuleInfo {
  author: string;
  homepage: string;
  license: string;
  path: string;
  isSystem: boolean; // true if in /etc/linuxio/modules/
  isSymlink: boolean; // true if path is symlink
  handlers: string[]; // List of registered handler names
  permissions: string[]; // Required permissions
  settings: SettingConfig[];
}

export interface InstallResult {
  success: boolean;
  moduleName: string;
  message: string;
}

export interface UninstallResult {
  success: boolean;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: string[];
  manifest?: {
    name: string;
    version: string;
    title: string;
    description: string;
    author?: string;
  };
}
