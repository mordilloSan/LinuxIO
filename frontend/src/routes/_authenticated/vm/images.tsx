import { createFileRoute } from "@tanstack/react-router";

import VMImagesPage from "./-components/VMImagesPage";

export const Route = createFileRoute("/_authenticated/vm/images")({
  component: VMImagesPage,
});
