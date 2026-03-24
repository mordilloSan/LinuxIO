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
  severity?: AlertSeverity;
  onClose?: () => void;
  action?: React.ReactNode;
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
      <div ref={ref} role="alert" className={cls} {...rest}>
        <Icon
          icon={SEVERITY_ICONS[severity]}
          className="app-alert__icon"
          width={22}
          height={22}
        />
        <div className="app-alert__message">{children}</div>
        {action && <div className="app-alert__action">{action}</div>}
        {onClose && (
          <button
            type="button"
            className="app-alert__close"
            onClick={onClose}
            aria-label="Close"
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </button>
        )}
      </div>
    );
  },
);

AppAlert.displayName = "AppAlert";

export default AppAlert;
