import "sonner";

import type { ToastMeta } from "@/types/navigation";

declare module "sonner" {
  interface ToastT {
    meta?: ToastMeta;
  }

  interface ExternalToast {
    meta?: ToastMeta;
  }
}
