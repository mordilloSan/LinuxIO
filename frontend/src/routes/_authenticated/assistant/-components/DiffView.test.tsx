import { describe, expect, it } from "vitest";

import { render } from "@/test/render";

import DiffView from "./DiffView";

describe("DiffView", () => {
  it("renders the path and added, removed and context lines", () => {
    const { container } = render(
      <DiffView
        newText={"keep\nnew\nend\n"}
        oldText={"keep\nold\nend\n"}
        path="/etc/app.conf"
      />,
    );
    expect(container).toHaveTextContent("/etc/app.conf");
    expect(container.querySelector(".assistant-diff__add")).toHaveTextContent(
      "+new",
    );
    expect(container.querySelector(".assistant-diff__del")).toHaveTextContent(
      "-old",
    );
    expect(container.querySelectorAll(".assistant-diff__ctx")).toHaveLength(2);
  });

  it("treats a missing oldText as a new file", () => {
    const { container } = render(<DiffView newText={"a\nb"} path="/x" />);
    expect(container.querySelectorAll(".assistant-diff__add")).toHaveLength(2);
    expect(container.querySelector(".assistant-diff__del")).toBeNull();
  });
});
