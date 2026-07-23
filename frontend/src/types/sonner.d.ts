import "sonner";

import type { ToastMeta } from "@/routes/-navigation";

declare module "sonner" {
  interface ToastT {
    meta?: ToastMeta;
  }

  interface ExternalToast {
    meta?: ToastMeta;
  }
}
