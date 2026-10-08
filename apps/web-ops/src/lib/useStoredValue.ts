"use client";

// Giá trị lưu trữ trình duyệt nhỏ, không bí mật, đọc qua useSyncExternalStore: an toàn khi hydrate
// (máy chủ trả undefined), cập nhật mọi nơi dùng khi ghi.
// - "local" (mặc định): tùy chọn giao diện không phải dữ liệu cá nhân (sidebar thu gọn).
// - "session": dữ liệu cá nhân (email ghi nhớ ở màn đăng nhập, M19) — mất khi đóng tab, không để lại
//   trên máy dùng chung. KHÔNG đặt dữ liệu cá nhân vào "local".
import { useSyncExternalStore } from "react";

export type StorageArea = "local" | "session";

const listeners = new Set<() => void>();

function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

function storageOf(area: StorageArea): Storage {
  return area === "session" ? window.sessionStorage : window.localStorage;
}

function read(key: string, area: StorageArea): string | null {
  try {
    return storageOf(area).getItem(key);
  } catch {
    return null;
  }
}

export function writeStoredValue(key: string, value: string | null, area: StorageArea = "local"): void {
  try {
    if (value === null) storageOf(area).removeItem(key);
    else storageOf(area).setItem(key, value);
  } catch {
    // bỏ qua: trình duyệt chặn lưu trữ
  }
  listeners.forEach((l) => l());
}

/** undefined khi render phía máy chủ / trước hydrate; null khi chưa có giá trị. */
export function useStoredValue(key: string, area: StorageArea = "local"): string | null | undefined {
  return useSyncExternalStore(
    subscribe,
    () => read(key, area),
    () => undefined,
  );
}
