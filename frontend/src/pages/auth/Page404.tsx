import { Link } from "react-router-dom";

import AppButton from "@/components/ui/AppButton";

function Page404() {
  return (
    <div style={{ textAlign: "center" }}>
      <h1 style={{ margin: 0, fontSize: "2rem", lineHeight: 1.25 }}>404</h1>
      <h2
        className="section-title"
        style={{ marginTop: 8, marginBottom: 8, textAlign: "center" }}
      >
        Page not found.
      </h2>
      <p
        className="text-muted"
        style={{ margin: 0, fontSize: "0.9286rem", lineHeight: 1.75 }}
      >
        The page you are looking for might have been removed.
      </p>

      <Link
        to="/"
        style={{
          textDecoration: "none",
          marginTop: 16,
          display: "inline-block",
        }}
      >
        <AppButton variant="contained" color="secondary">
          Return to website
        </AppButton>
      </Link>
    </div>
  );
}

export default Page404;
