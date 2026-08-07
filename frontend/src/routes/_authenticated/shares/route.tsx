import { createFileRoute } from "@tanstack/react-router";

import { makeTabLayout } from "@/components/tabbar";
import { ShareIcon } from "@/icons/svg";

import { SHARES_TABS } from "./-components/sharesTabs";

const SharesLayout = makeTabLayout(SHARES_TABS);

export const Route = createFileRoute("/_authenticated/shares")({
  component: SharesLayout,
  staticData: {
    navigation: {
      icon: ShareIcon,
      position: 70,
      title: "Shares",
    },
  },
});
