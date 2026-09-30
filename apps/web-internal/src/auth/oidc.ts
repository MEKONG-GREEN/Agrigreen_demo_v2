"use client";

import { InMemoryWebStorage, UserManager, WebStorageStateStore } from "oidc-client-ts";
import { config } from "@/config";

let manager: UserManager | null = null;

/**
 * OIDC Authorization Code + PKCE (S256) với Keycloak, client công khai agrigreen-web-internal.
 * - Token chỉ nằm trong BỘ NHỚ (InMemoryWebStorage). Tải lại trang → đăng nhập im lặng qua phiên SSO.
 * - sessionStorage chỉ giữ state + code_verifier của một lượt chuyển hướng.
 * - acr_values không ép ở đây: màn hình quản trị (MFA) yêu cầu đăng nhập lại có OTP — Keycloak cấu hình
 *   luồng bước-lên; api-internal bề mặt quản trị từ chối token không có amr otp (mfa_required).
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
      stateStore: new WebStorageStateStore({ store: window.sessionStorage, prefix: "agi.oidc." }),
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
