"use client";
import React, { useState, Suspense } from "react";

import dynamic from "next/dynamic";

import { useAuth } from "@/contexts/AuthContext";
import LoadingIndicator from "@/components/LoadingIndicator";

const LoginForm = dynamic(
  () => import("@/app/(blank-layout-pages)/login/LoginForm"),
  { ssr: true },
);

function LoginPage() {
  const { login, error } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);

  const handleClickShowPassword = () => {
    setShowPassword(!showPassword);
  };

  const handleFormSubmit = async (event) => {
    event.preventDefault();
    await login(username, password);
  };

  return (
    <LoginForm
      username={username}
      setUsername={setUsername}
      password={password}
      setPassword={setPassword}
      showPassword={showPassword}
      handleClickShowPassword={handleClickShowPassword}
      handleMouseDownPassword={handleFormSubmit}
      handleFormSubmit={handleFormSubmit}
      error={error}
    />
  );
}

export default function LoginPageWrapper() {
  return (
    <Suspense fallback={<LoadingIndicator />}>
      <LoginPage />
    </Suspense>
  );
}
