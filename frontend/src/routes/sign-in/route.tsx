import { createFileRoute } from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import { requireGuest } from "@/routes/-context";
import { optionalString } from "@/routes/-search";

import AuthLayout from "./-components/AuthLayout";
import LoginPage from "./-components/LoginPage";

export const Route = createFileRoute("/sign-in")({
  validateSearch: (search) => ({
    ...optionalString(search, "redirect"),
  }),
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
