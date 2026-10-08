"use client";

// "Ghi nhớ đăng nhập" (M19): chỉ khi người dùng TỰ tích chọn; chỉ nhớ EMAIL (không bao giờ nhớ SĐT);
// lưu ở sessionStorage (hết khi đóng tab) và xóa khi đăng xuất. Phiên do Keycloak giữ, không lưu bí mật.
import { useStoredValue, writeStoredValue } from "./useStoredValue";

export const REMEMBER_KEY = "ag.login.identifier";

function isEmail(identifier: string): boolean {
  return /^[^\s@]+@[^\s@]+$/.test(identifier.trim());
}

/** Bản cũ lưu email/SĐT ở localStorage — dọn khi gặp. */
export function purgeLegacyIdentifier(): void {
  writeStoredValue(REMEMBER_KEY, null, "local");
}

/** Gọi khi gửi thẻ Đăng nhập: chỉ lưu email khi `on`; SĐT hoặc bỏ tích → xóa. */
export function rememberIdentifier(identifier: string, on: boolean): void {
  purgeLegacyIdentifier();
  writeStoredValue(REMEMBER_KEY, on && isEmail(identifier) ? identifier.trim() : null, "session");
}

/** Gọi khi đăng xuất. */
export function forgetIdentifier(): void {
  purgeLegacyIdentifier();
  writeStoredValue(REMEMBER_KEY, null, "session");
}

/** Email đã ghi nhớ: undefined trước hydrate, null khi không có. */
export function useRememberedIdentifier(): string | null | undefined {
  return useStoredValue(REMEMBER_KEY, "session");
}
