import type { Middleware } from "openapi-fetch";

/**
 * Sinh UUIDv7 (RFC 9562): 48 bit thời gian Unix ms + 74 bit ngẫu nhiên.
 * Dùng cho Idempotency-Key (R-51) và id do client sinh (R-11).
 */
export function uuidv7(now: number = Date.now()): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let ts = Math.floor(now);
  for (let i = 5; i >= 0; i--) {
    bytes[i] = ts & 0xff;
    ts = Math.floor(ts / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70; // phiên bản 7
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80; // biến thể RFC
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** Khóa hợp lệ theo contracts/openapi: ^[A-Za-z0-9._:-]{8,200}$ */
export const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9._:-]{8,200}$/;

export const IDEMPOTENCY_HEADER = "Idempotency-Key";

export function newIdempotencyKey(): string {
  return uuidv7();
}

/**
 * Middleware: mọi POST đều mang Idempotency-Key. Nếu nơi gọi đã đặt khóa (vd để
 * thử lại đúng một thao tác sau lỗi mạng) thì giữ nguyên khóa đó.
 */
export function idempotencyMiddleware(generate: () => string = newIdempotencyKey): Middleware {
  return {
    onRequest({ request }) {
      if (request.method.toUpperCase() === "POST" && !request.headers.get(IDEMPOTENCY_HEADER)) {
        request.headers.set(IDEMPOTENCY_HEADER, generate());
      }
      return request;
    },
  };
}
