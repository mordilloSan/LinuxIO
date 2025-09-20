// src/utils/axios.ts
import axios, { AxiosError } from "axios";

const isDev = import.meta.env.DEV;

// Resolve API base URL.
// - Dev: use VITE_API_URL if provided, otherwise rely on the Vite proxy (empty string).
// - Prod: fall back to VITE_API_URL (typically "/") so requests stay same-origin.
const devApi = import.meta.env.VITE_API_URL as string | undefined;
const baseURL = isDev ? devApi || "" : import.meta.env.VITE_API_URL ?? "";

const axiosInstance = axios.create({
  baseURL,
  withCredentials: true,
});

if (isDev) {
  // eslint-disable-next-line no-console
  console.info(`[axios] baseURL: ${baseURL || "(vite proxy)"}`);
}

let isAuthRedirect = false;

function onSignInPage() {
  // Normalize pathname: strip trailing slashes
  const path = window.location.pathname.replace(/\/+$/, "");
  return path === "/sign-in";
}

axiosInstance.interceptors.response.use(
  (r) => r,
  (err: AxiosError) => {
    const res = err.response;
    const url = err.config?.url || "";
    if (!res) return Promise.reject(err);

    if (res.status === 401) {
      // Never redirect for auth endpoints themselves
      if (
        url.includes("/auth/me") ||
        url.includes("/auth/login") ||
        url.includes("/auth/logout")
      ) {
        return Promise.reject(err);
      }

      // If we’re already on the sign-in page (with or without ?redirect=...), do nothing.
      if (onSignInPage()) {
        return Promise.reject(err);
      }

      // Avoid multiple concurrent redirects
      if (isAuthRedirect) return Promise.reject(err);
      isAuthRedirect = true;

      // Build a single stable redirect target
      const params = new URLSearchParams(window.location.search);
      const existing = params.get("redirect");
      const current =
        window.location.pathname +
        window.location.search +
        window.location.hash;
      const target = existing || current;

      const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;
      // Replace avoids history pileup
      window.location.replace(to);
      // Return a never-resolving promise to halt the original call chain
      return new Promise(() => {});
    }

    return Promise.reject(err);
  },
);

export default axiosInstance;
