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
