import type { ConfigHardwareSections } from "@/api";

export const defaultHardwareSections = {
  overview: true,
  hardware: true,
  sensors: true,
  systemInfo: true,
  gpu: true,
  pciDevices: true,
  memoryModules: true,
} satisfies ConfigHardwareSections;

export function resolvedHardwareSections(
  sections: ConfigHardwareSections | undefined,
): Required<ConfigHardwareSections> {
  return { ...defaultHardwareSections, ...(sections ?? {}) };
}
