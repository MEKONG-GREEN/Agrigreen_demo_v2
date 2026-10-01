"use client";

// Giá trị localStorage nhỏ, không bí mật (sidebar thu gọn, email/SĐT ghi nhớ) đọc qua useSyncExternalStore:
// an toàn khi hydrate (máy chủ trả undefined), cập nhật mọi nơi dùng khi ghi.
import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();

function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writeStoredValue(key: string, value: string | null): void {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // bỏ qua: trình duyệt chặn lưu trữ
  }
  listeners.forEach((l) => l());
}

/** undefined khi render phía máy chủ / trước hydrate; null khi chưa có giá trị. */
export function useStoredValue(key: string): string | null | undefined {
  return useSyncExternalStore(
    subscribe,
    () => read(key),
    () => undefined,
  );
}
