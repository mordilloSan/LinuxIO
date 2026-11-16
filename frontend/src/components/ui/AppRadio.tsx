import type { InputHTMLAttributes, Ref } from "react";

import "./app-radio.css";

export interface AppRadioProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "size" | "type"
> {
  ref?: Ref<HTMLInputElement>;
  size?: "small" | "medium";
}

/** A native radio input with the app's mark; give each group one `name`. */
const AppRadio = ({
  ref,
  size = "medium",
  className,
  ...rest
}: AppRadioProps) => {
  const cls = ["app-radio", size === "small" && "app-radio--small", className]
    .filter(Boolean)
    .join(" ");

  return (
    <span className={cls}>
      <input className="app-radio__input" ref={ref} type="radio" {...rest} />
      <svg
        aria-hidden="true"
        className="app-radio__icon"
        focusable="false"
        viewBox="0 0 24 24"
      >
        <path
          className="app-radio__ring"
          d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8z"
        />
        <path
          className="app-radio__dot"
          d="M12 7c-2.76 0-5 2.24-5 5s2.24 5 5 5 5-2.24 5-5-2.24-5-5-5z"
        />
      </svg>
    </span>
  );
};

AppRadio.displayName = "AppRadio";

export default AppRadio;
