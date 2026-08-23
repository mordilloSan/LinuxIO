import { Icon } from "@iconify/react";
import { type HTMLAttributes, type ReactNode, type Ref } from "react";

import "./app-alert.css";

type AlertSeverity = "info" | "warning" | "error" | "success";

const SEVERITY_ICONS: Record<AlertSeverity, string> = {
  info: "mdi:information-outline",
  warning: "mdi:alert-outline",
  error: "mdi:alert-circle-outline",
  success: "mdi:check-circle-outline",
};

export interface AppAlertProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "action"
> {
  action?: ReactNode;
  onClose?: () => void;
  ref?: Ref<HTMLDivElement>;
  severity?: AlertSeverity;
}

export const AppAlertTitle = ({ children }: { children: ReactNode }) => (
  <div className="app-alert__title">{children}</div>
);

const AppAlert = ({
  ref,
  severity = "info",
  onClose,
  action,
  children,
  className,
  ...rest
}: AppAlertProps) => {
  const cls = ["app-alert", `app-alert--${severity}`, className]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={cls} ref={ref} role="alert" {...rest}>
      <Icon
        className="app-alert__icon"
        height={22}
        icon={SEVERITY_ICONS[severity]}
        width={22}
      />
      <div className="app-alert__message">{children}</div>
      {action && <div className="app-alert__action">{action}</div>}
      {onClose && (
        <button
          aria-label="Close"
          className="app-alert__close"
          onClick={onClose}
          type="button"
        >
          <Icon height={18} icon="mdi:close" width={18} />
        </button>
      )}
    </div>
  );
};

AppAlert.displayName = "AppAlert";

export default AppAlert;
