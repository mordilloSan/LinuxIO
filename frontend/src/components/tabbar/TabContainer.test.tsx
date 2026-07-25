import { screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { render } from "@/test/render";
import type { TabConfig } from "@/types/tabcontainer";

import TabContainer from "./TabContainer";

type TestTab = "users" | "groups";

const tabs = [
  { value: "users", label: "Users", component: <div>Users content</div> },
  {
    value: "groups",
    label: "Groups",
    component: <div>Groups content</div>,
  },
] satisfies readonly TabConfig<TestTab>[];

function TestTabs({ initialTab = "users" }: { initialTab?: TestTab }) {
  const [activeTab, setActiveTab] = useState<TestTab>(initialTab);
  return (
    <TabContainer
      activeTab={activeTab}
      onTabChange={setActiveTab}
      tabs={tabs}
    />
  );
}

describe("TabContainer", () => {
  it("only mounts the active tab while switching", async () => {
    const { user } = render(<TestTabs />);

    expect(await screen.findByText("Users content")).toBeInTheDocument();
    expect(screen.queryByText("Groups content")).not.toBeInTheDocument();

    await user.click(await screen.findByRole("tab", { name: "Groups" }));

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(screen.getByText("Groups content")).toBeInTheDocument();
  });

  it("mounts the tab selected by its owner", async () => {
    render(<TestTabs initialTab="groups" />);

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(await screen.findByText("Groups content")).toBeInTheDocument();
  });
});
