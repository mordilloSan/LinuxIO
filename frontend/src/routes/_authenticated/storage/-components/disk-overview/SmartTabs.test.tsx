import { describe, expect, it, vi } from "vitest";

import type { SmartData } from "@/api";
import { render, screen } from "@/test/render";

import { OverviewTab } from "./OverviewTab";
import { SelfTestsTab } from "./SelfTestsTab";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const smart: SmartData = {
  smart_status: "PASSED",
  temperature_celsius: 42,
  power_on_time: { hours: 49 },
  power_cycle_count: 12,
  ata_smart_self_test_log: {
    standard: {
      table: [
        {
          num: 7,
          type: { string: "Extended offline", value: 2 },
          status: { string: "Completed without error", passed: true, value: 0 },
          lifetime_hours: 48,
        },
      ],
    },
  },
};

describe("typed SMART views", () => {
  it("preserves temperature and power counters in the overview", () => {
    render(
      <OverviewTab
        drive={{
          name: "sda",
          model: "Drive",
          sizeBytes: 1024,
          transport: "sata",
          smart,
        }}
      />,
    );
    expect(screen.getByText("42°C")).toBeInTheDocument();
    expect(screen.getByText("2d 1h")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
  });

  it("renders cached self-test history and its original test number", () => {
    render(
      <SelfTestsTab
        onRunTest={() => {}}
        selfTestLog={smart.ata_smart_self_test_log}
        smartmontoolsAvailable
        startPending={null}
      />,
    );
    expect(screen.getByText("Extended offline")).toBeInTheDocument();
    expect(screen.getByText("Completed without error")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
  });
});
