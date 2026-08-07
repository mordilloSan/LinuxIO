import { describe, expect, it } from "vitest";

import { WebVitalsFooterStats } from "@/components/dev-tools/WebVitalsFooterStats";
import { render, screen } from "@/test/render";

describe("WebVitalsFooterStats", () => {
  it("renders every Core Web Vital in the footer", () => {
    render(<WebVitalsFooterStats />);

    const stats = screen.getByLabelText("Web Vitals");
    expect(stats).toHaveTextContent("CLS");
    expect(stats).toHaveTextContent("INP");
    expect(stats).toHaveTextContent("LCP");
  });
});
