// src/utils/axios.ts
import axios, { AxiosError } from "axios";

const axiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
  withCredentials: true,
});

let isAuthRedirect = false;

function onSignInPage() {
  // Normalize pathname: strip trailing slashes
  const path = window.location.pathname.replace(/\/+$/, "");
  return path === "/sign-in";
}

function redirectToSignIn() {
  if (onSignInPage() || isAuthRedirect) {
    return false;
  }

  isAuthRedirect = true;

  const params = new URLSearchParams(window.location.search);
  const existing = params.get("redirect");
  const current =
    window.location.pathname + window.location.search + window.location.hash;
  const target = existing || current;

  const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;
  window.location.replace(to);
  return true;
}

function isAuthEndpoint(url: string) {
  return (
    url.includes("/auth/me") ||
    url.includes("/auth/login") ||
    url.includes("/auth/logout")
  );
}

axiosInstance.interceptors.response.use(
  (r) => r,
  (err: AxiosError) => {
    const res = err.response;
    const url = err.config?.url || "";
    if (!res) return Promise.reject(err);

    const rawError =
      typeof res.data === "object" && res.data !== null
        ? (res.data as any).error
        : undefined;
    const normalizedError =
      typeof rawError === "string" ? rawError.toLowerCase() : "";

    const bridgeDown =
      res.status >= 500 && normalizedError.includes("bridge unavailable");

    if (bridgeDown) {
      if (redirectToSignIn()) {
        return new Promise(() => {});
      }
      return Promise.reject(err);
    }

    if (res.status === 401) {
      // Never redirect for auth endpoints themselves
      if (isAuthEndpoint(url)) {
        return Promise.reject(err);
      }
      if (redirectToSignIn()) {
        return new Promise(() => {});
      }
    }

    return Promise.reject(err);
  },
);

export default axiosInstance;
