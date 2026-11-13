import type { SectionPage } from "@/hooks/useSectionToggles";

/**
 * The collapsible sections of the hardware page, in page order. It lives
 * outside HardwarePage so the route loader, which skips the prefetches of a
 * collapsed section, does not pull the page into its chunk.
 */
export const HARDWARE_SECTIONS = {
  key: "hardware",
  sections: [
    "systemInfo",
    "hardware",
    "sensors",
    "memoryModules",
    "pciDevices",
  ],
} as const satisfies SectionPage<string>;
