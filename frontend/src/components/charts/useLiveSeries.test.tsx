import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import { useLiveSeries } from "./useLiveSeries";

const Probe = ({
  fetchHistory,
  id,
}: {
  fetchHistory: Parameters<typeof useLiveSeries>[1];
  id: string;
}) => {
  useLiveSeries([id], fetchHistory);
  return null;
};

describe("useLiveSeries backfill", () => {
  it("asks for history only in a session with sudo", () => {
    const withoutSudo = vi.fn(async () => ({}));
    render(<Probe fetchHistory={withoutSudo} id="test:no-sudo" />, {
      auth: { privileged: false },
      capabilities: { monitoringAvailable: true },
    });
    expect(withoutSudo).not.toHaveBeenCalled();

    const withSudo = vi.fn(async () => ({}));
    render(<Probe fetchHistory={withSudo} id="test:sudo" />, {
      auth: { privileged: true },
      capabilities: { monitoringAvailable: true },
    });
    expect(withSudo).toHaveBeenCalledTimes(1);
  });
});
