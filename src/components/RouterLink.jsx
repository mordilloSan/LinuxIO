"use client";

// React Imports
import { forwardRef } from "react";

// Next Imports
import Link from "next/link";

// Forward the `href` and other props to the `Link` component from Next.js
const RouterLink = forwardRef((props, ref) => {
  const { href, className, children, ...other } = props;

  return (
    <Link href={href} passHref legacyBehavior>
      <a ref={ref} className={className} {...other}>
        {children}
      </a>
    </Link>
  );
});

RouterLink.displayName = "RouterLink";

export default RouterLink;
