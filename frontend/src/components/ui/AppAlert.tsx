import { Icon } from "@iconify/react";
import React from "react";

import "./app-alert.css";

type AlertSeverity = "info" | "warning" | "error" | "success";

const SEVERITY_ICONS: Record<AlertSeverity, string> = {
  info: "mdi:information-outline",
  warning: "mdi:alert-outline",
  error: "mdi:alert-circle-outline",
  success: "mdi:check-circle-outline",
};

export interface AppAlertProps extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  "action"
> {
  action?: React.ReactNode;
  onClose?: () => void;
  severity?: AlertSeverity;
}

export const AppAlertTitle: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => <div className="app-alert__title">{children}</div>;

const AppAlert = React.forwardRef<HTMLDivElement, AppAlertProps>(
  (
    { severity = "info", onClose, action, children, className, ...rest },
    ref,
  ) => {
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
  },
);

AppAlert.displayName = "AppAlert";

export default AppAlert;
