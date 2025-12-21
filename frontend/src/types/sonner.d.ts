import "sonner";

declare module "sonner" {
  interface ToastT {
    meta?: {
      href?: string;
      label?: string;
    };
  }

  interface ExternalToast {
    meta?: {
      href?: string;
      label?: string;
    };
  }
}
