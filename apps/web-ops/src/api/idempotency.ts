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

// ---------------------------------------------------------------------------------------------------------------------
// P0-WEB-IDEMKEY (#129): MỘT khóa cho mỗi thao tác của người dùng.
// Khóa gắn với "dấu vân tay" của yêu cầu (phương thức + đường dẫn + ngữ cảnh + thân). Bấm lại sau lỗi mạng/5xx/hết giờ
// → cùng thân → cùng khóa → máy chủ trả lại kết quả cũ thay vì tạo bản ghi thứ hai (409 + node mồ côi). Thành công hoặc
// lỗi nghiệp vụ dứt khoát (4xx) → bỏ khóa: lần gửi sau là thao tác mới. Đổi dữ liệu form → thân khác → khóa mới.
// ---------------------------------------------------------------------------------------------------------------------

/** Kết quả một lần gửi: mã HTTP (+ code problem+json) hoặc "network" (không tới được máy chủ / hết giờ). */
export type AttemptOutcome = { status: number; code?: string | null } | "network";

/**
 * Chưa biết máy chủ đã xử lý hay chưa → GIỮ khóa để lần bấm lại được phát lại: lỗi mạng, hết giờ (408), 425, 429,
 * 5xx, và idempotency_in_progress (lần gửi trước còn đang chạy). Còn lại (2xx, 4xx nghiệp vụ) là kết quả dứt khoát.
 */
export function outcomeUnknown(o: AttemptOutcome): boolean {
  if (o === "network") return true;
  if (o.code === "idempotency_in_progress") return true;
  return o.status === 0 || o.status === 408 || o.status === 425 || o.status === 429 || o.status >= 500;
}

/** Dấu vân tay yêu cầu ghi: cùng thao tác bấm lại ⇔ cùng dấu. */
export function requestFingerprint(method: string, url: string, context: string | null | undefined, body: string): string {
  let path = url;
  try {
    const u = new URL(url, "http://local");
    path = `${u.pathname}${u.search}`;
  } catch {
    /* giữ nguyên */
  }
  return `${method.toUpperCase()} ${path} ctx=${context ?? ""} ${body}`;
}

const MAX_PENDING = 64;

/**
 * Một "phiên thao tác" (form/màn hình): giữ khóa cho các yêu cầu CHƯA có kết quả dứt khoát, theo dấu vân tay.
 * - keyFor(fp): khóa đang treo cho fp, hoặc khóa mới (uuidv7) nếu chưa có.
 * - settle(fp, kết quả): kết quả dứt khoát → bỏ khóa (lần sau là thao tác mới); chưa rõ → giữ để thử lại.
 * - reset(): bỏ mọi khóa (vd đóng/mở lại form muốn bắt đầu thao tác mới).
 */
export class IdempotentOperation {
  private readonly pending = new Map<string, string>();

  constructor(private readonly generate: () => string = newIdempotencyKey) {}

  keyFor(fingerprint: string): string {
    const k = this.pending.get(fingerprint);
    if (k) return k;
    const key = this.generate();
    this.pending.set(fingerprint, key);
    while (this.pending.size > MAX_PENDING) this.pending.delete(this.pending.keys().next().value as string);
    return key;
  }

  /** Khóa đang treo (để kiểm thử/hiển thị), không sinh mới. */
  peek(fingerprint: string): string | undefined {
    return this.pending.get(fingerprint);
  }

  settle(fingerprint: string, outcome: AttemptOutcome): void {
    if (!outcomeUnknown(outcome)) this.pending.delete(fingerprint);
  }

  reset(): void {
    this.pending.clear();
  }

  get size(): number {
    return this.pending.size;
  }
}

let activeOperation: IdempotentOperation | null = null;

/**
 * Chạy fn với op là phiên thao tác hiện hành. Client đọc phiên ĐỒNG BỘ lúc bắt đầu gửi (trước await đầu tiên), nên
 * mọi lời gọi ghi khởi phát ngay trong fn (kể cả lời gọi đầu tiên sau `async () => { await api.x() }`) dùng khóa
 * của op. Lời gọi nằm SAU một await khác trong fn không còn trong phạm vi → bọc lại bằng withIdempotentOperation
 * (useAction truyền sẵn hàm `idem` cho việc này).
 */
export function withIdempotentOperation<T>(op: IdempotentOperation, fn: () => T): T {
  const prev = activeOperation;
  activeOperation = op;
  try {
    return fn();
  } finally {
    activeOperation = prev;
  }
}

export function currentIdempotentOperation(): IdempotentOperation | null {
  return activeOperation;
}

/** Một lần gửi: khóa dùng + báo kết quả cho phiên thao tác. */
export interface IdempotencyAttempt {
  key: string;
  settle(outcome: AttemptOutcome): void;
}

/**
 * Chọn khóa cho một lần gửi: khóa nơi gọi truyền tường minh > khóa của phiên thao tác (theo dấu vân tay) > khóa mới
 * (chỉ cho lời gọi một lần, không có nút "thử lại").
 */
export function beginAttempt(op: IdempotentOperation | null, fingerprint: string, explicit?: string | null): IdempotencyAttempt {
  if (explicit) return { key: explicit, settle: () => undefined };
  if (!op) return { key: newIdempotencyKey(), settle: () => undefined };
  return { key: op.keyFor(fingerprint), settle: (o) => op.settle(fingerprint, o) };
}

async function problemCode(res: Response): Promise<string | null> {
  if (res.ok) return null;
  try {
    const j = (await res.clone().json()) as { code?: unknown };
    return typeof j?.code === "string" ? j.code : null;
  } catch {
    return null;
  }
}

const WRITE_METHODS = new Set(["POST", "PUT", "PATCH"]);
/** Phiên thao tác bắt được lúc yêu cầu khởi phát (theo id yêu cầu của openapi-fetch). */
const capturedOps = new Map<string, IdempotentOperation>();

/**
 * Middleware đặt ĐẦU TIÊN trong client openapi-fetch: onRequest của middleware đầu chạy đồng bộ trong lời gọi
 * api.POST(...), nên bắt được phiên thao tác hiện hành trước khi các middleware async (Bearer) chen await.
 */
export function idempotencyScopeMiddleware(): Middleware {
  return {
    onRequest({ request, id }) {
      const op = currentIdempotentOperation();
      if (op && WRITE_METHODS.has(request.method.toUpperCase())) capturedOps.set(id, op);
      return undefined;
    },
  };
}

/**
 * Middleware: mọi POST đều mang Idempotency-Key. Trong phiên thao tác (useAction / useIdempotentAction) khóa lấy từ
 * phiên theo dấu vân tay (POST/PUT/PATCH) và kết quả được báo lại để giữ/bỏ khóa. Ngoài phiên: nếu nơi gọi đã đặt
 * khóa (vd để thử lại đúng một thao tác sau lỗi mạng) thì giữ nguyên khóa đó, không thì sinh khóa mới.
 */
export function idempotencyMiddleware(generate: () => string = newIdempotencyKey): Middleware {
  const attempts = new Map<string, IdempotencyAttempt>();
  return {
    async onRequest({ request, id }) {
      const method = request.method.toUpperCase();
      const op = capturedOps.get(id) ?? currentIdempotentOperation();
      capturedOps.delete(id);
      if (op && WRITE_METHODS.has(method)) {
        const body = await request.clone().text();
        const att = beginAttempt(op, requestFingerprint(method, request.url, request.headers.get("X-AG-Context"), body));
        attempts.set(id, att);
        request.headers.set(IDEMPOTENCY_HEADER, att.key);
        return request;
      }
      if (method === "POST" && !request.headers.get(IDEMPOTENCY_HEADER)) {
        request.headers.set(IDEMPOTENCY_HEADER, generate());
      }
      return request;
    },
    async onResponse({ response, id }) {
      const att = attempts.get(id);
      if (att) {
        attempts.delete(id);
        att.settle({ status: response.status, code: await problemCode(response) });
      }
      return undefined;
    },
    onError({ id }) {
      const att = attempts.get(id);
      if (att) {
        attempts.delete(id);
        att.settle("network");
      }
      return undefined;
    },
  };
}
