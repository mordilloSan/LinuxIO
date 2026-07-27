import { createFileRoute } from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import { requireGuest, sanitizeInternalRedirect } from "@/routes/-auth";

import AuthLayout from "./-components/AuthLayout";
import LoginPage from "./-components/LoginPage";

export const Route = createFileRoute("/sign-in")({
  validateSearch: (search) => {
    const redirect = sanitizeInternalRedirect(search.redirect);
    return redirect ? { redirect } : {};
  },
  beforeLoad: ({ context, location }) => requireGuest(context, location.search),
  component: SignInScreen,
});

function SignInScreen() {
  return (
    <AuthLayout>
      <LoginPage />
      <BootstrapLoaderReady />
    </AuthLayout>
  );
}
