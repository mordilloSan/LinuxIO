// src/utils/axios.ts
import axios, { AxiosError } from "axios";

const axiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
  withCredentials: true,
});

let isAuthRedirect = false;

axiosInstance.interceptors.response.use(
  (r) => r,
  (err: AxiosError) => {
    const res = err.response;
    const url = err.config?.url || "";
    if (!res) return Promise.reject(err);

    if (res.status === 401) {
      // never redirect on auth endpoints
      if (
        url.includes("/auth/me") ||
        url.includes("/auth/login") ||
        url.includes("/auth/logout")
      ) {
        return Promise.reject(err);
      }
      // already on sign-in? do nothing
      if (window.location.pathname === "/sign-in") {
        return Promise.reject(err);
      }
      // avoid multiple concurrent redirects
      if (isAuthRedirect) return Promise.reject(err);
      isAuthRedirect = true;

      const params = new URLSearchParams(window.location.search);
      const existing = params.get("redirect");
      // build the target ONCE; if a redirect already exists, reuse it
      const target =
        existing ||
        `${window.location.pathname}${window.location.search}${window.location.hash}`;

      const to = `/sign-in${target ? `?redirect=${encodeURIComponent(target)}` : ""}`;
      window.location.replace(to);
      return new Promise(() => {});
    }
    return Promise.reject(err);
  },
);

export default axiosInstance;
