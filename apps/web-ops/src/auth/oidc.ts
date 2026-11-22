"use client";

import { InMemoryWebStorage, UserManager, WebStorageStateStore, type UserManagerSettings } from "oidc-client-ts";
import { config } from "@/config";

let manager: UserManager | null = null;

/**
 * Scope xin khi đăng nhập. Mỗi scope (trừ "openid") phải là default/optional client scope của client
 * trong realm (deploy/compose/keycloak/agrigreen-realm.json), nếu không Keycloak từ chối authorize
 * (invalid_scope). "profile" của realm chỉ ánh xạ preferred_username; tenant/vai đến từ scope mặc
 * định agrigreen-claims/roles nên không cần xin thêm. Kiểm trong oidc.test.ts.
 */
export const OIDC_SCOPES = ["openid", "profile"] as const;

/** Tham số UserManager (tách để kiểm redirect URI/scope mà không tạo UserManager). */
export function oidcSettings(origin: string): Omit<UserManagerSettings, "userStore" | "stateStore"> {
  return {
    authority: config.oidc.issuer,
    client_id: config.oidc.clientId,
    redirect_uri: `${origin}/login/callback`,
    silent_redirect_uri: `${origin}/login/silent`,
    post_logout_redirect_uri: `${origin}/login`,
    response_type: "code",
    scope: OIDC_SCOPES.join(" "),
    automaticSilentRenew: true,
    monitorSession: false,
    loadUserInfo: false,
  };
}

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
    manager = new UserManager({
      ...oidcSettings(window.location.origin),
      userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
      stateStore: new WebStorageStateStore({ store: window.sessionStorage, prefix: "ag.oidc." }),
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
