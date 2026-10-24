import "sonner";

import type { ToastMeta } from "@/api";

declare module "sonner" {
  interface ToastT {
    meta?: ToastMeta;
  }

  interface ExternalToast {
    meta?: ToastMeta;
  }
}
