"use client";

import { InMemoryWebStorage, UserManager, WebStorageStateStore } from "oidc-client-ts";
import { config } from "@/config";

let manager: UserManager | null = null;

/**
 * OIDC Authorization Code + PKCE (S256) với Keycloak, client công khai agrigreen-web-ops.
 * - Token (access/id/refresh) chỉ nằm trong BỘ NHỚ (InMemoryWebStorage) — không localStorage.
 *   Tải lại trang → đăng nhập im lặng (prompt=none) qua phiên SSO của Keycloak.
 * - sessionStorage chỉ giữ state + code_verifier tạm thời của một lượt chuyển hướng,
 *   bị xoá ngay sau callback.
 */
export function getUserManager(): UserManager {
  if (typeof window === "undefined") throw new Error("UserManager chỉ dùng trên trình duyệt");
  if (!manager) {
    const origin = window.location.origin;
    manager = new UserManager({
      authority: config.oidc.issuer,
      client_id: config.oidc.clientId,
      redirect_uri: `${origin}/login/callback`,
      silent_redirect_uri: `${origin}/login/silent`,
      post_logout_redirect_uri: `${origin}/login`,
      response_type: "code",
      scope: "openid profile",
      userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
      stateStore: new WebStorageStateStore({ store: window.sessionStorage, prefix: "ag.oidc." }),
      automaticSilentRenew: true,
      monitorSession: false,
      loadUserInfo: false,
    });
  }
  return manager;
}

/** Chỉ cho phép quay về đường dẫn nội bộ (chống open redirect). */
export function safeReturnTo(value: unknown): string {
  if (typeof value !== "string") return "/";
  if (!value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\")) return "/";
  if (value.startsWith("/login")) return "/";
  return value;
}
